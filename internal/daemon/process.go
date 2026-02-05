package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/7c/gopm/internal/logwriter"
	"github.com/7c/gopm/internal/protocol"
)

// Process is the daemon-internal representation of a managed process.
type Process struct {
	mu       sync.Mutex
	info     protocol.ProcessInfo
	cmd      *exec.Cmd
	exitCh   chan struct{}
	stopping bool
	stdout   *logwriter.TimestampWriter
	stderr   *logwriter.TimestampWriter

	// Metrics tracking
	lastTicks  uint64
	lastSample time.Time
}

// NewProcess creates a new Process from StartParams.
func NewProcess(id int, params protocol.StartParams) *Process {
	policy := protocol.DefaultRestartPolicy()

	if params.AutoRestart != "" {
		policy.AutoRestart = protocol.AutoRestartMode(params.AutoRestart)
	}
	if params.MaxRestarts != nil {
		policy.MaxRestarts = *params.MaxRestarts
	}
	if params.MinUptime != "" {
		if d, err := time.ParseDuration(params.MinUptime); err == nil {
			policy.MinUptime = protocol.Duration{Duration: d}
		}
	}
	if params.RestartDelay != "" {
		if d, err := time.ParseDuration(params.RestartDelay); err == nil {
			policy.RestartDelay = protocol.Duration{Duration: d}
		}
	}
	if params.ExpBackoff {
		policy.ExpBackoff = true
	}
	if params.MaxDelay != "" {
		if d, err := time.ParseDuration(params.MaxDelay); err == nil {
			policy.MaxDelay = protocol.Duration{Duration: d}
		}
	}
	if params.KillTimeout != "" {
		if d, err := time.ParseDuration(params.KillTimeout); err == nil {
			policy.KillTimeout = protocol.Duration{Duration: d}
		}
	}

	name := params.Name
	if name == "" {
		name = filepath.Base(params.Command)
	}

	cwd := params.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	var maxLogSize int64 = 1048576 // 1MB
	if params.MaxLogSize != "" {
		if s, err := protocol.ParseSize(params.MaxLogSize); err == nil {
			maxLogSize = s
		}
	}

	logOut := params.LogOut
	if logOut == "" {
		logOut = filepath.Join(protocol.LogDir(), fmt.Sprintf("%s-out.log", name))
	}
	logErr := params.LogErr
	if logErr == "" {
		logErr = filepath.Join(protocol.LogDir(), fmt.Sprintf("%s-err.log", name))
	}

	return &Process{
		info: protocol.ProcessInfo{
			ID:            id,
			Name:          name,
			Command:       params.Command,
			Args:          params.Args,
			Cwd:           cwd,
			Env:           params.Env,
			Interpreter:   params.Interpreter,
			Status:        protocol.StatusStopped,
			RestartPolicy: policy,
			CreatedAt:     time.Now(),
			LogOut:        logOut,
			LogErr:        logErr,
			MaxLogSize:    maxLogSize,
		},
	}
}

// Info returns a copy of the process info (thread-safe).
func (p *Process) Info() protocol.ProcessInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	info := p.info
	if info.Args == nil {
		info.Args = []string{}
	}
	if info.Env == nil {
		info.Env = map[string]string{}
	}
	return info
}

// Start launches the process.
func (p *Process) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Ensure log directory exists
	os.MkdirAll(filepath.Dir(p.info.LogOut), 0755)

	// Set up log writers with timestamps
	var err error
	outRot, err := logwriter.New(p.info.LogOut, p.info.MaxLogSize, 3)
	if err != nil {
		return fmt.Errorf("open stdout log: %w", err)
	}
	errRot, err := logwriter.New(p.info.LogErr, p.info.MaxLogSize, 3)
	if err != nil {
		outRot.Close()
		return fmt.Errorf("open stderr log: %w", err)
	}
	p.stdout = logwriter.NewTimestampWriter(outRot)
	p.stderr = logwriter.NewTimestampWriter(errRot)

	// Build command
	var cmd *exec.Cmd
	if p.info.Interpreter != "" {
		args := append([]string{p.info.Command}, p.info.Args...)
		cmd = exec.Command(p.info.Interpreter, args...)
	} else {
		cmd = exec.Command(p.info.Command, p.info.Args...)
	}

	cmd.Dir = p.info.Cwd
	cmd.Stdout = p.stdout
	cmd.Stderr = p.stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Build environment
	if len(p.info.Env) > 0 {
		env := os.Environ()
		for k, v := range p.info.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = env
	}

	if err := cmd.Start(); err != nil {
		p.stdout.Underlying().Close()
		p.stderr.Underlying().Close()
		return fmt.Errorf("start process: %w", err)
	}

	p.cmd = cmd
	p.exitCh = make(chan struct{})
	p.stopping = false
	p.info.PID = cmd.Process.Pid
	p.info.Status = protocol.StatusOnline
	p.info.StatusReason = ""
	p.info.Uptime = time.Now()
	p.lastSample = time.Now()
	p.lastTicks = 0

	return nil
}

// Stop sends SIGTERM then SIGKILL after timeout.
func (p *Process) Stop() error {
	p.mu.Lock()
	if p.info.Status != protocol.StatusOnline || p.cmd == nil {
		p.mu.Unlock()
		return nil
	}
	p.stopping = true
	pid := p.info.PID
	exitCh := p.exitCh
	killTimeout := p.info.RestartPolicy.KillTimeout.Duration
	killSignal := p.info.RestartPolicy.KillSignal
	p.mu.Unlock()

	if killTimeout == 0 {
		killTimeout = 5 * time.Second
	}
	if killSignal == 0 {
		killSignal = int(syscall.SIGTERM)
	}

	// Send kill signal to process group
	syscall.Kill(-pid, syscall.Signal(killSignal))

	select {
	case <-exitCh:
		return nil
	case <-time.After(killTimeout):
		// Escalate to SIGKILL
		syscall.Kill(-pid, syscall.SIGKILL)
		<-exitCh
		return nil
	}
}

// Wait blocks until the process exits. Returns the exit code.
func (p *Process) Wait() int {
	err := p.cmd.Wait()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		exitCode = -1
	}
	return exitCode
}

// MarkExited updates process state after exit.
func (p *Process) MarkExited(exitCode int, status protocol.Status) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.info.ExitCode = exitCode
	p.info.PID = 0
	p.info.CPU = 0
	p.info.Memory = 0
	p.info.Status = status
}

// SetReason sets the status reason (why a process stopped/errored).
func (p *Process) SetReason(reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.info.StatusReason = reason
}

// LogAction writes a daemon action message to the process's stderr log.
// Messages are prefixed with [gopm] and get a timestamp from TimestampWriter.
func (p *Process) LogAction(format string, args ...interface{}) {
	p.mu.Lock()
	w := p.stderr
	p.mu.Unlock()
	if w == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	w.Write([]byte("[gopm] " + msg + "\n"))
}

// CloseLogWriters closes the log writers.
func (p *Process) CloseLogWriters() {
	if p.stdout != nil {
		p.stdout.Underlying().Close()
	}
	if p.stderr != nil {
		p.stderr.Underlying().Close()
	}
}

// FlushLogs truncates the log files.
func (p *Process) FlushLogs() error {
	if p.stdout != nil {
		if err := p.stdout.Underlying().Truncate(); err != nil {
			return err
		}
	}
	if p.stderr != nil {
		if err := p.stderr.Underlying().Truncate(); err != nil {
			return err
		}
	}
	return nil
}

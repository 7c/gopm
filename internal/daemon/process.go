package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/7c/gopm/internal/logwriter"
	"github.com/7c/gopm/internal/protocol"
)

// zombieDetections is a package-level counter incremented whenever Start()
// hits the safety-net branch (a previous cmd was unreaped). Read from the
// Daemon side for telemetry.
var zombieDetections uint64

// ZombieDetections returns the current value of the zombie counter.
func ZombieDetections() uint64 { return atomic.LoadUint64(&zombieDetections) }

// Process is the daemon-internal representation of a managed process.
type Process struct {
	mu       sync.Mutex
	info     protocol.ProcessInfo
	cmd      *exec.Cmd
	exitCh   chan struct{}
	stopping bool
	stdout   *logwriter.TimestampWriter
	stderr   *logwriter.TimestampWriter

	// instance is incremented every Start(). Monitor goroutines capture this
	// at creation time and use it to detect stale restart paths. Atomic so
	// it can be read without holding p.mu.
	instance int64

	// cancelRestart, when closed, signals the supervisor restart delay to
	// abort a pending auto-restart. Replaced on every Start().
	cancelRestart chan struct{}

	// Lifecycle counters — mirrored into ProcessInfo at Info() time.
	// Guarded by p.mu.
	startCount             int
	stopCount              int
	crashCount             int
	userRestartCount       int
	supervisorRestartCount int
	memoryPeak             uint64
	logBytesWritten        int64
	logRotations           int

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

	var maxLogSize int64 = 104857600 // 100MB
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
	if info.Listeners == nil {
		info.Listeners = []string{}
	}
	// Mirror internal counters into the outward-facing struct so they
	// appear in JSON IPC responses, dump.json, and telegraf emissions.
	info.StartCount = p.startCount
	info.StopCount = p.stopCount
	info.CrashCount = p.crashCount
	info.UserRestartCount = p.userRestartCount
	info.SupervisorRestartCount = p.supervisorRestartCount
	info.Instance = p.instance
	info.MemoryPeak = p.memoryPeak
	info.LogBytesWritten = p.logBytesWritten
	info.LogRotations = p.logRotations
	return info
}

// Instance returns the current instance counter (atomic, lock-free).
func (p *Process) Instance() int64 {
	return atomic.LoadInt64(&p.instance)
}

// Start launches the process. reason is a short label describing who
// initiated the start (e.g. "user-start", "user-restart", "supervisor-restart",
// "resurrect") and is included in logs.
func (p *Process) Start(reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	prevInstance := p.instance
	prevPID := p.info.PID

	// Safety net: if p.cmd still has a live underlying OS process that
	// we never waited on, a previous cleanup path failed to reap it.
	// This should NOT happen in normal flow: every Start() call is
	// preceded either by Stop() (which waits for the monitor to reap)
	// or by a supervisor restart (where the monitor has already reaped
	// the crashed process). If we hit this branch, log a WARN with
	// enough context to debug the misbehaving call site.
	if p.cmd != nil && p.cmd.Process != nil && p.cmd.ProcessState == nil {
		atomic.AddUint64(&zombieDetections, 1)
		zombiePID := p.cmd.Process.Pid
		slog.Warn("UNEXPECTED: zombie cmd at Start — a caller skipped Stop() or a monitor never ran",
			"name", p.info.Name, "reason", reason,
			"zombie_pid", zombiePID, "prev_instance", prevInstance)
		// Best effort: kill the whole process group. The existing monitor
		// (if any) will reap it when cmd.Wait() returns. If there is no
		// monitor, this process will stay as a zombie until the daemon
		// dies — but it cannot accumulate: subsequent Start() calls hit
		// this same branch and the WARN above will fire each time.
		if err := syscall.Kill(-zombiePID, syscall.SIGKILL); err != nil {
			slog.Warn("failed to kill zombie process group",
				"name", p.info.Name, "zombie_pid", zombiePID, "error", err)
		}
	}

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
	// Surface rotation events in daemon.log so operators can correlate
	// follower hiccups with rotation. DEBUG level keeps default logs clean.
	name := p.info.Name
	outRot.OnRotate = func(path string, n int) {
		slog.Debug("log rotated", "process", name, "stream", "stdout", "path", path, "rotations", n)
	}
	errRot.OnRotate = func(path string, n int) {
		slog.Debug("log rotated", "process", name, "stream", "stderr", "path", path, "rotations", n)
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
		slog.Error("cmd.Start failed",
			"name", p.info.Name, "reason", reason, "error", err)
		return fmt.Errorf("start process: %w", err)
	}

	atomic.AddInt64(&p.instance, 1)
	p.cmd = cmd
	p.exitCh = make(chan struct{})
	p.cancelRestart = make(chan struct{})
	p.stopping = false
	p.info.PID = cmd.Process.Pid
	p.info.Status = protocol.StatusOnline
	p.info.StatusReason = ""
	p.info.Uptime = time.Now()
	p.info.InRestartDelay = false
	p.lastSample = time.Now()
	p.lastTicks = 0
	p.memoryPeak = 0 // reset peak for new instance

	// Lifecycle counters.
	p.startCount++
	switch reason {
	case "user-restart":
		p.userRestartCount++
	case "supervisor-restart":
		p.supervisorRestartCount++
	}

	slog.Info("process started",
		"name", p.info.Name, "reason", reason,
		"new_pid", p.info.PID, "instance", p.instance,
		"prev_pid", prevPID, "prev_instance", prevInstance)

	return nil
}

// Stop sends SIGTERM then SIGKILL after timeout. If the process is currently
// in the supervisor restart-delay window (status != Online but a supervisor
// goroutine is waiting to restart), Stop also cancels the pending restart.
func (p *Process) Stop() error {
	p.mu.Lock()
	p.stopCount++

	// Always cancel any pending supervisor restart. Doing this first ensures
	// that even if the process is already "stopped" from the supervisor's
	// point of view, the upcoming Start() call gets aborted.
	if p.cancelRestart != nil {
		select {
		case <-p.cancelRestart:
			// already cancelled/closed
		default:
			close(p.cancelRestart)
			slog.Info("cancelled pending supervisor restart",
				"name", p.info.Name, "instance", p.instance)
		}
	}

	if p.info.Status != protocol.StatusOnline || p.cmd == nil {
		status := p.info.Status
		p.mu.Unlock()
		slog.Debug("Stop called but process not Online, no kill needed",
			"name", p.info.Name, "status", status)
		return nil
	}
	p.stopping = true
	pid := p.info.PID
	exitCh := p.exitCh
	killTimeout := p.info.RestartPolicy.KillTimeout.Duration
	killSignal := p.info.RestartPolicy.KillSignal
	instance := p.instance
	p.mu.Unlock()

	if killTimeout == 0 {
		killTimeout = 5 * time.Second
	}
	if killSignal == 0 {
		killSignal = int(syscall.SIGTERM)
	}

	slog.Info("Stop sending signal to process group",
		"name", p.info.Name, "pid", pid, "signal", killSignal,
		"instance", instance, "kill_timeout", killTimeout)

	// Send kill signal to process group
	syscall.Kill(-pid, syscall.Signal(killSignal))

	select {
	case <-exitCh:
		slog.Info("Stop: process exited cleanly",
			"name", p.info.Name, "pid", pid, "instance", instance)
		return nil
	case <-time.After(killTimeout):
		// Escalate to SIGKILL
		slog.Warn("Stop: kill timeout expired, escalating to SIGKILL",
			"name", p.info.Name, "pid", pid, "instance", instance)
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

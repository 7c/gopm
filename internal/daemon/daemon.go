package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// Version is set at build time.
var Version = "dev"

// Daemon manages child processes and handles CLI requests.
type Daemon struct {
	mu        sync.RWMutex
	processes map[string]*Process // keyed by name
	nextID    int
	listener  net.Listener
	startTime time.Time
	stopCh    chan struct{}
	home      string
}

// Run starts the daemon. This is the main entry point for daemon mode.
func Run(version string) {
	Version = version
	home := protocol.GopmHome()
	os.MkdirAll(home, 0755)
	os.MkdirAll(protocol.LogDir(), 0755)

	// Set up logging to a file
	logPath := filepath.Join(home, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot open log file: %v\n", err)
		os.Exit(1)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// Write PID file
	pidPath := protocol.PIDFilePath()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		slog.Error("cannot write PID file", "error", err)
		os.Exit(1)
	}

	d := &Daemon{
		processes: make(map[string]*Process),
		startTime: time.Now(),
		stopCh:    make(chan struct{}),
		home:      home,
	}

	// Start socket listener
	sockPath := protocol.SocketPath()
	os.Remove(sockPath) // remove stale socket
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		slog.Error("cannot listen on socket", "error", err)
		os.Exit(1)
	}
	d.listener = listener
	os.Chmod(sockPath, 0700)

	slog.Info("daemon started", "pid", os.Getpid(), "socket", sockPath, "version", Version)

	// Auto-load saved process list from dump.json
	if resurrected, err := d.ResurrectProcesses(); err != nil {
		slog.Error("failed to resurrect processes on startup", "error", err)
	} else if len(resurrected) > 0 {
		slog.Info("auto-resurrected processes on startup", "count", len(resurrected))
	}

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		slog.Info("received shutdown signal")
		d.shutdown()
	}()

	// Start metrics sampling
	go d.sampleMetrics()

	// Accept connections
	d.acceptLoop()
}

func (d *Daemon) acceptLoop() {
	for {
		conn, err := d.listener.Accept()
		if err != nil {
			select {
			case <-d.stopCh:
				return
			default:
				slog.Error("accept error", "error", err)
				continue
			}
		}
		go d.handleConnection(conn)
	}
}

func (d *Daemon) handleConnection(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	if !scanner.Scan() {
		return
	}

	var req protocol.Request
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		resp := protocol.Response{Error: "invalid request: " + err.Error()}
		data, _ := json.Marshal(resp)
		fmt.Fprintf(conn, "%s\n", data)
		return
	}

	resp := d.handleRequest(req)
	data, _ := json.Marshal(resp)
	fmt.Fprintf(conn, "%s\n", data)
}

func (d *Daemon) handleRequest(req protocol.Request) protocol.Response {
	switch req.Method {
	case protocol.MethodPing:
		return d.handlePing()
	case protocol.MethodStart:
		return d.handleStart(req.Params)
	case protocol.MethodStop:
		return d.handleStop(req.Params)
	case protocol.MethodRestart:
		return d.handleRestart(req.Params)
	case protocol.MethodDelete:
		return d.handleDelete(req.Params)
	case protocol.MethodList:
		return d.handleList()
	case protocol.MethodDescribe:
		return d.handleDescribe(req.Params)
	case protocol.MethodIsRunning:
		return d.handleIsRunning(req.Params)
	case protocol.MethodLogs:
		return d.handleLogs(req.Params)
	case protocol.MethodFlush:
		return d.handleFlush(req.Params)
	case protocol.MethodSave:
		return d.handleSave()
	case protocol.MethodResurrect:
		return d.handleResurrect()
	case protocol.MethodKill:
		return d.handleKill()
	default:
		return errorResponse(fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func (d *Daemon) handlePing() protocol.Response {
	result := protocol.PingResult{
		PID:           os.Getpid(),
		Uptime:        protocol.FormatDuration(time.Since(d.startTime)),
		UptimeSeconds: int64(time.Since(d.startTime).Seconds()),
		Version:       Version,
	}
	return successResponse(result)
}

func (d *Daemon) handleStart(params json.RawMessage) protocol.Response {
	var sp protocol.StartParams
	if err := json.Unmarshal(params, &sp); err != nil {
		return errorResponse("invalid start params: " + err.Error())
	}
	if sp.Command == "" {
		return errorResponse("command is required")
	}

	proc, err := d.startProcess(sp)
	if err != nil {
		return errorResponse(err.Error())
	}
	return successResponse(proc.Info())
}

func (d *Daemon) startProcess(params protocol.StartParams) (*Process, error) {
	d.mu.Lock()

	name := params.Name
	if name == "" {
		name = filepath.Base(params.Command)
	}

	// Check for duplicate name
	if _, exists := d.processes[name]; exists {
		d.mu.Unlock()
		return nil, fmt.Errorf("process %q already exists", name)
	}

	id := d.nextID
	d.nextID++
	d.mu.Unlock()

	proc := NewProcess(id, params)

	if err := proc.Start(); err != nil {
		return nil, err
	}

	d.mu.Lock()
	d.processes[proc.info.Name] = proc
	d.mu.Unlock()

	go d.monitor(proc)

	slog.Info("process started", "name", proc.info.Name, "pid", proc.info.PID, "id", id)
	return proc, nil
}

func (d *Daemon) handleStop(params json.RawMessage) protocol.Response {
	target, err := parseTarget(params)
	if err != nil {
		return errorResponse(err.Error())
	}

	procs := d.resolveTarget(target)
	if len(procs) == 0 {
		return errorResponse(fmt.Sprintf("process %q not found", target))
	}

	for _, p := range procs {
		if err := p.Stop(); err != nil {
			slog.Error("failed to stop process", "name", p.info.Name, "error", err)
		}
	}

	return successResponse(map[string]bool{"success": true})
}

func (d *Daemon) handleRestart(params json.RawMessage) protocol.Response {
	target, err := parseTarget(params)
	if err != nil {
		return errorResponse(err.Error())
	}

	procs := d.resolveTarget(target)
	if len(procs) == 0 {
		return errorResponse(fmt.Sprintf("process %q not found", target))
	}

	var results []protocol.ProcessInfo
	for _, p := range procs {
		p.Stop()

		p.mu.Lock()
		p.info.Restarts = 0
		p.mu.Unlock()

		p.CloseLogWriters()
		if err := p.Start(); err != nil {
			slog.Error("failed to restart process", "name", p.info.Name, "error", err)
			continue
		}
		go d.monitor(p)
		results = append(results, p.Info())
	}

	if len(results) == 1 {
		return successResponse(results[0])
	}
	return successResponse(results)
}

func (d *Daemon) handleDelete(params json.RawMessage) protocol.Response {
	target, err := parseTarget(params)
	if err != nil {
		return errorResponse(err.Error())
	}

	procs := d.resolveTarget(target)
	if len(procs) == 0 {
		return errorResponse(fmt.Sprintf("process %q not found", target))
	}

	for _, p := range procs {
		p.Stop()
		p.CloseLogWriters()
		d.mu.Lock()
		delete(d.processes, p.info.Name)
		d.mu.Unlock()
		slog.Info("process deleted", "name", p.info.Name)
	}

	return successResponse(map[string]bool{"success": true})
}

func (d *Daemon) handleList() protocol.Response {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var infos []protocol.ProcessInfo
	for _, p := range d.processes {
		infos = append(infos, p.Info())
	}

	// Sort by ID
	for i := 0; i < len(infos); i++ {
		for j := i + 1; j < len(infos); j++ {
			if infos[i].ID > infos[j].ID {
				infos[i], infos[j] = infos[j], infos[i]
			}
		}
	}

	if infos == nil {
		infos = []protocol.ProcessInfo{}
	}

	return successResponse(infos)
}

func (d *Daemon) handleDescribe(params json.RawMessage) protocol.Response {
	target, err := parseTarget(params)
	if err != nil {
		return errorResponse(err.Error())
	}

	proc := d.findProcess(target)
	if proc == nil {
		return errorResponse(fmt.Sprintf("process %q not found", target))
	}

	return successResponse(proc.Info())
}

func (d *Daemon) handleIsRunning(params json.RawMessage) protocol.Response {
	target, err := parseTarget(params)
	if err != nil {
		return errorResponse(err.Error())
	}

	proc := d.findProcess(target)
	if proc == nil {
		result := protocol.IsRunningResult{
			Name:    target,
			Running: false,
			Status:  "",
		}
		return successResponse(result)
	}

	info := proc.Info()
	result := protocol.IsRunningResult{
		Name:     info.Name,
		Running:  info.Status == protocol.StatusOnline,
		Status:   info.Status,
		PID:      info.PID,
		Uptime:   protocol.FormatDuration(time.Since(info.Uptime)),
		ExitCode: info.ExitCode,
		Restarts: info.Restarts,
	}
	return successResponse(result)
}

func (d *Daemon) handleLogs(params json.RawMessage) protocol.Response {
	var lp protocol.LogsParams
	if err := json.Unmarshal(params, &lp); err != nil {
		return errorResponse("invalid logs params: " + err.Error())
	}

	lines := lp.Lines
	if lines <= 0 {
		lines = 20
	}

	// Support "all" target by aggregating logs from every process.
	if lp.Target == "all" {
		d.mu.RLock()
		var parts []string
		for _, p := range d.processes {
			info := p.Info()
			logPath := info.LogOut
			if lp.ErrOnly {
				logPath = info.LogErr
			}
			content, err := tailFile(logPath, lines)
			if err != nil {
				continue
			}
			if content != "" {
				header := fmt.Sprintf("==> %s <==", info.Name)
				parts = append(parts, header+"\n"+content)
			}
		}
		d.mu.RUnlock()
		combined := strings.Join(parts, "\n\n")
		return successResponse(map[string]interface{}{
			"content":  combined,
			"log_path": "",
		})
	}

	proc := d.findProcess(lp.Target)
	if proc == nil {
		return errorResponse(fmt.Sprintf("process %q not found", lp.Target))
	}

	info := proc.Info()
	logPath := info.LogOut
	if lp.ErrOnly {
		logPath = info.LogErr
	}

	content, err := tailFile(logPath, lines)
	if err != nil {
		return errorResponse(fmt.Sprintf("read logs: %v", err))
	}

	return successResponse(map[string]interface{}{
		"content":  content,
		"log_path": logPath,
	})
}

func (d *Daemon) handleFlush(params json.RawMessage) protocol.Response {
	target, err := parseTarget(params)
	if err != nil {
		return errorResponse(err.Error())
	}

	procs := d.resolveTarget(target)
	if len(procs) == 0 {
		return errorResponse(fmt.Sprintf("process %q not found", target))
	}

	for _, p := range procs {
		if err := p.FlushLogs(); err != nil {
			slog.Error("failed to flush logs", "name", p.info.Name, "error", err)
		}
	}

	return successResponse(map[string]bool{"success": true})
}

func (d *Daemon) handleSave() protocol.Response {
	if err := d.SaveState(); err != nil {
		return errorResponse(err.Error())
	}
	d.mu.RLock()
	count := len(d.processes)
	d.mu.RUnlock()
	return successResponse(map[string]interface{}{"saved": true, "count": count})
}

func (d *Daemon) handleResurrect() protocol.Response {
	resurrected, err := d.ResurrectProcesses()
	if err != nil {
		return errorResponse(err.Error())
	}
	if resurrected == nil {
		resurrected = []protocol.ProcessInfo{}
	}
	return successResponse(resurrected)
}

func (d *Daemon) handleKill() protocol.Response {
	go func() {
		time.Sleep(100 * time.Millisecond)
		d.shutdown()
	}()
	return successResponse(map[string]string{"status": "daemon stopping"})
}

func (d *Daemon) shutdown() {
	slog.Info("daemon shutting down")

	// Stop accepting connections
	d.listener.Close()
	close(d.stopCh)

	// Stop all processes in parallel
	d.mu.RLock()
	var wg sync.WaitGroup
	for _, p := range d.processes {
		wg.Add(1)
		go func(proc *Process) {
			defer wg.Done()
			proc.Stop()
			proc.CloseLogWriters()
		}(p)
	}
	d.mu.RUnlock()
	wg.Wait()

	// Save state
	d.SaveState()

	// Cleanup
	os.Remove(protocol.SocketPath())
	os.Remove(protocol.PIDFilePath())

	slog.Info("daemon stopped")
	os.Exit(0)
}

// resolveTarget finds processes matching a target string (name, id, or "all").
func (d *Daemon) resolveTarget(target string) []*Process {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if target == "all" {
		procs := make([]*Process, 0, len(d.processes))
		for _, p := range d.processes {
			procs = append(procs, p)
		}
		return procs
	}

	// Try by name
	if p, ok := d.processes[target]; ok {
		return []*Process{p}
	}

	// Try by ID
	id, err := strconv.Atoi(target)
	if err == nil {
		for _, p := range d.processes {
			if p.info.ID == id {
				return []*Process{p}
			}
		}
	}

	return nil
}

// findProcess finds a single process by name or ID.
func (d *Daemon) findProcess(target string) *Process {
	procs := d.resolveTarget(target)
	if len(procs) == 1 {
		return procs[0]
	}
	return nil
}

func parseTarget(params json.RawMessage) (string, error) {
	var tp protocol.TargetParams
	if err := json.Unmarshal(params, &tp); err != nil {
		return "", fmt.Errorf("invalid target params: %w", err)
	}
	if tp.Target == "" {
		return "", fmt.Errorf("target is required")
	}
	return tp.Target, nil
}

func successResponse(data interface{}) protocol.Response {
	raw, _ := json.Marshal(data)
	return protocol.Response{Success: true, Data: raw}
}

func errorResponse(msg string) protocol.Response {
	return protocol.Response{Error: msg}
}

// tailFile reads the last N lines from a file.
func tailFile(path string, n int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	// Remove trailing empty line
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n"), nil
}

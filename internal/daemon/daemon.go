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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/7c/gopm/internal/config"
	"github.com/7c/gopm/internal/logwriter"
	"github.com/7c/gopm/internal/mcphttp"
	"github.com/7c/gopm/internal/protocol"
	"github.com/7c/gopm/internal/telemetry"
)

// Version is set at build time.
var Version = "dev"

// parseLogLevel resolves the daemon log level. Default is Debug; --log-level
// can override with info|warn|error. The legacy --debug flag forces Debug.
func parseLogLevel(arg string, legacyDebug bool) slog.Level {
	if legacyDebug {
		return slog.LevelDebug
	}
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "", "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	default:
		return slog.LevelDebug
	}
}

func logLevelSource(arg string, legacyDebug bool) string {
	if legacyDebug {
		return "--debug"
	}
	if arg == "" {
		return "default"
	}
	return "--log-level"
}

// daemonCounters tracks daemon-wide lifecycle and RPC counters. All fields
// are accessed atomically via sync/atomic where possible, or under
// Daemon.mu for the map.
type daemonCounters struct {
	rpcCallsByMethod  map[string]uint64 // guarded by Daemon.mu
	rpcErrors         uint64            // atomic
	stateSaves        uint64            // atomic
	stateSaveFailures uint64            // atomic
	resurrectCount    uint64            // atomic
	zombieDetections  uint64            // atomic
	monitorStales     uint64            // atomic
	restartCancels    uint64            // atomic
}

// Daemon manages child processes and handles CLI requests.
type Daemon struct {
	mu        sync.RWMutex
	processes map[string]*Process // keyed by name
	nextID    int
	listener  net.Listener
	startTime time.Time
	stopCh    chan struct{}
	home      string

	counters daemonCounters

	mcpServer    *mcphttp.Server
	telegraf     *telemetry.TelegrafEmitter
	snapshots map[string]*snapshotRing // per-process metrics history

	resolved     *config.Resolved
	configPath   string
	configSource string

	logWriter *logwriter.RotatingWriter

	stateSaveLogMu   sync.Mutex
	stateSaveLogLast time.Time
	stateSaveLogSeen int
}

// Run starts the daemon. This is the main entry point for daemon mode.
// logLevelArg is the --log-level argument value. Empty string selects the
// default (debug). The legacy debug bool is preserved for backward compat.
func Run(version string, configFlag string, debug bool, logLevelArg string) {
	Version = version
	home := protocol.GopmHome()
	os.MkdirAll(home, 0755)

	// Load configuration
	result, err := config.Load(home, configFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		os.Exit(1)
	}
	resolved, warnings, err := config.Resolve(result.Config, home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: gopm.config.json: %s\n", err)
		os.Exit(1)
	}

	// Ensure log directory exists (may come from config)
	os.MkdirAll(resolved.LogDir, 0755)

	// Set up logging to a file with size-based rotation using the same
	// max_size/max_files knobs as child process logs. Default is Debug so
	// production daemons capture enough context to diagnose issues without
	// a redeploy. Override via --log-level (info|warn|error). Legacy --debug
	// is a no-op now since debug is the default.
	logPath := filepath.Join(home, "daemon.log")
	logWriter, err := logwriter.New(logPath, resolved.LogMaxSize, resolved.LogMaxFiles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot open log file: %v\n", err)
		os.Exit(1)
	}
	logLevel := parseLogLevel(logLevelArg, debug)
	logger := slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)
	slog.Info("log level set", "level", logLevel.String(),
		"source", logLevelSource(logLevelArg, debug))

	// Log config warnings
	for _, w := range warnings {
		slog.Warn(w)
	}

	// Write PID file
	pidPath := protocol.PIDFilePath()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		slog.Error("cannot write PID file", "error", err)
		os.Exit(1)
	}

	d := &Daemon{
		processes:    make(map[string]*Process),
		snapshots:    make(map[string]*snapshotRing),
		startTime:    time.Now(),
		stopCh:       make(chan struct{}),
		home:         home,
		resolved:     resolved,
		configPath:   result.Path,
		configSource: result.Source,
		logWriter:    logWriter,
		counters: daemonCounters{
			rpcCallsByMethod: make(map[string]uint64),
		},
	}
	logWriter.OnRotate = func(path string, rotations int) {
		// Must not call slog synchronously: this callback fires from inside
		// a slog.Handler.Handle call, which holds the handler's own mutex
		// across the write. Re-entering slog here would deadlock on that
		// mutex and wedge every subsequent logger, freezing the daemon.
		go slog.Info("daemon log rotated", "path", path, "rotations", rotations)
	}

	// Print startup banner
	d.printBanner(resolved, result.Path, result.Source)

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

	// Start MCP HTTP server if enabled
	if resolved.MCPEnabled {
		var bindAddrs []mcphttp.BindAddr
		for _, ba := range resolved.MCPBindAddrs {
			bindAddrs = append(bindAddrs, mcphttp.BindAddr{Addr: ba.Addr, Label: ba.Label})
		}
		srv := mcphttp.New(d, bindAddrs, resolved.MCPURI, logger)
		if err := srv.Start(bindAddrs); err != nil {
			slog.Error("MCP HTTP server failed to start", "error", err)
		} else {
			d.mcpServer = srv
		}
	}

	// Start telegraf telemetry if enabled
	if resolved.TelegrafEnabled && resolved.TelegrafAddr != nil {
		em, err := telemetry.NewTelegrafEmitter(resolved.TelegrafAddr, resolved.TelegrafMeas)
		if err != nil {
			slog.Warn("telegraf emitter failed to start, continuing without telemetry", "error", err)
		} else {
			d.telegraf = em
			slog.Info("telegraf telemetry started", "addr", resolved.TelegrafAddr.String())
		}
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

	// Start listener scanning
	go d.scanListeners()

	// Accept connections
	d.acceptLoop()
}

// printBanner logs the startup banner with resolved configuration.
func (d *Daemon) printBanner(r *config.Resolved, configPath, configSource string) {
	configLine := "(none found, using defaults)"
	if configPath != "" {
		configLine = fmt.Sprintf("%s (%s)", configPath, configSource)
	}

	mcpLine := "disabled"
	if r.MCPEnabled {
		var binds []string
		for _, ba := range r.MCPBindAddrs {
			binds = append(binds, fmt.Sprintf("%s (%s)", ba.Addr, ba.Label))
		}
		mcpLine = fmt.Sprintf("enabled on %s, URI: %s", strings.Join(binds, ", "), r.MCPURI)
	}

	telegrafLine := "disabled"
	if r.TelegrafEnabled && r.TelegrafAddr != nil {
		telegrafLine = fmt.Sprintf("enabled, UDP: %s, measurement: %s", r.TelegrafAddr.String(), r.TelegrafMeas)
	}

	slog.Info("GoPM starting",
		"version", Version,
		"pid", os.Getpid(),
		"config", configLine,
		"home", d.home,
		"log_dir", r.LogDir,
		"log_max_size", r.LogMaxSize,
		"log_max_files", r.LogMaxFiles,
		"mcp", mcpLine,
		"telegraf", telegrafLine,
	)
}

// HandleRequest is the exported entry point for internal callers (e.g. MCP HTTP).
func (d *Daemon) HandleRequest(req protocol.Request) protocol.Response {
	return d.handleRequest(req)
}

// ProcessCount returns counts of processes by status.
func (d *Daemon) ProcessCount() (total, online, stopped, errored int) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, p := range d.processes {
		info := p.Info()
		total++
		switch info.Status {
		case protocol.StatusOnline:
			online++
		case protocol.StatusStopped:
			stopped++
		case protocol.StatusErrored:
			errored++
		}
	}
	return
}

// DaemonUptime returns how long the daemon has been running.
func (d *Daemon) DaemonUptime() time.Duration {
	return time.Since(d.startTime)
}

// DaemonPID returns the daemon's process ID.
func (d *Daemon) DaemonPID() int {
	return os.Getpid()
}

// DaemonVersion returns the daemon version string.
func (d *Daemon) DaemonVersion() string {
	return Version
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

	for scanner.Scan() {
		var req protocol.Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			slog.Debug("invalid request from client", "error", err)
			resp := protocol.Response{Error: "invalid request: " + err.Error()}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(conn, "%s\n", data)
			return
		}

		slog.Debug("request received", "method", req.Method)
		resp := d.handleRequest(req)
		data, _ := json.Marshal(resp)
		slog.Debug("response sent", "method", req.Method, "success", resp.Success, "bytes", len(data))
		fmt.Fprintf(conn, "%s\n", data)
	}
}

func (d *Daemon) handleRequest(req protocol.Request) protocol.Response {
	d.mu.Lock()
	d.counters.rpcCallsByMethod[req.Method]++
	d.mu.Unlock()

	var resp protocol.Response
	switch req.Method {
	case protocol.MethodPing:
		resp = d.handlePing()
	case protocol.MethodStart:
		resp = d.handleStart(req.Params)
	case protocol.MethodStop:
		resp = d.handleStop(req.Params)
	case protocol.MethodRestart:
		resp = d.handleRestart(req.Params)
	case protocol.MethodDelete:
		resp = d.handleDelete(req.Params)
	case protocol.MethodList:
		resp = d.handleList()
	case protocol.MethodDescribe:
		resp = d.handleDescribe(req.Params)
	case protocol.MethodIsRunning:
		resp = d.handleIsRunning(req.Params)
	case protocol.MethodLogs:
		resp = d.handleLogs(req.Params)
	case protocol.MethodFlush:
		resp = d.handleFlush(req.Params)
	case protocol.MethodSave:
		resp = d.handleSave()
	case protocol.MethodResurrect:
		resp = d.handleResurrect()
	case protocol.MethodKill:
		resp = d.handleKill()
	case protocol.MethodReboot:
		resp = d.handleReboot()
	case protocol.MethodStats:
		resp = d.handleStats(req.Params)
	default:
		resp = errorResponse(fmt.Sprintf("unknown method: %s", req.Method))
	}
	if !resp.Success {
		atomic.AddUint64(&d.counters.rpcErrors, 1)
	}
	return resp
}

func (d *Daemon) handlePing() protocol.Response {
	result := protocol.PingResult{
		PID:          os.Getpid(),
		Uptime:       protocol.FormatDuration(time.Since(d.startTime)),
		UptimeMs:     time.Since(d.startTime).Milliseconds(),
		Version:      Version,
		ConfigFile:   d.configPath,
		ConfigSource: d.configSource,
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
	if err := d.SaveState(); err != nil {
		slog.Error("auto-save failed after start", "error", err)
	}
	return successResponse(proc.Info())
}

func (d *Daemon) startProcess(params protocol.StartParams) (*Process, error) {
	return d.startProcessWithReason(params, "user-start")
}

func (d *Daemon) startProcessWithReason(params protocol.StartParams, reason string) (*Process, error) {
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

	if err := proc.Start(reason); err != nil {
		return nil, err
	}

	d.mu.Lock()
	d.processes[proc.info.Name] = proc
	d.mu.Unlock()

	proc.LogAction("process started (PID %d)", proc.info.PID)

	go d.monitor(proc)

	slog.Info("startProcess: process registered",
		"name", proc.info.Name, "pid", proc.info.PID, "id", id, "reason", reason)
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
		if err := p.Stop("stopped by user"); err != nil {
			slog.Error("failed to stop process", "name", p.info.Name, "error", err)
		}
	}

	if err := d.SaveState(); err != nil {
		slog.Error("auto-save failed after stop", "error", err)
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
		p.Stop("restarting")

		p.mu.Lock()
		p.info.Restarts = 0
		p.mu.Unlock()

		p.CloseLogWriters()
		if err := p.Start("user-restart"); err != nil {
			slog.Error("failed to restart process", "name", p.info.Name, "error", err)
			continue
		}
		go d.monitor(p)
		results = append(results, p.Info())
	}

	if err := d.SaveState(); err != nil {
		slog.Error("auto-save failed after restart", "error", err)
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
		p.Stop("deleted")
		p.CloseLogWriters()
		d.mu.Lock()
		delete(d.processes, p.info.Name)
		delete(d.snapshots, p.info.Name)
		d.mu.Unlock()
		slog.Info("process deleted", "name", p.info.Name)
	}

	if err := d.SaveState(); err != nil {
		slog.Error("auto-save failed after delete", "error", err)
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
		logPaths := make(map[string]string) // name → log file path
		for _, p := range d.processes {
			info := p.Info()
			logPath := info.LogOut
			if lp.ErrOnly {
				logPath = info.LogErr
			}
			logPaths[info.Name] = logPath
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
			"content":   combined,
			"log_path":  "",
			"log_paths": logPaths,
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

func (d *Daemon) handleReboot() protocol.Response {
	// Save state while processes are still online so dump.json records them
	// as "online". Then stop everything and exit — systemd (or the CLI)
	// will restart the daemon which auto-resurrects from the saved dump.
	if err := d.SaveState(); err != nil {
		return errorResponse(fmt.Sprintf("save failed: %v", err))
	}

	d.mu.RLock()
	count := len(d.processes)
	d.mu.RUnlock()

	go func() {
		time.Sleep(100 * time.Millisecond)
		d.rebootShutdown()
	}()
	return successResponse(map[string]interface{}{
		"status": "rebooting",
		"saved":  count,
	})
}

// rebootShutdown stops everything and exits WITHOUT re-saving state.
// The caller must have already saved state with online processes.
func (d *Daemon) rebootShutdown() {
	slog.Info("daemon rebooting (save-and-exit)")

	// Stop MCP HTTP server
	if d.mcpServer != nil {
		d.mcpServer.Shutdown()
	}

	// Stop telegraf
	if d.telegraf != nil {
		d.telegraf.Close()
	}

	// Signal goroutines to stop before closing listener so acceptLoop
	// sees stopCh closed and exits without logging spurious errors.
	close(d.stopCh)
	d.listener.Close()

	// Stop all processes in parallel
	d.mu.RLock()
	var wg sync.WaitGroup
	for _, p := range d.processes {
		wg.Add(1)
		go func(proc *Process) {
			defer wg.Done()
			proc.Stop("daemon reboot")
			proc.CloseLogWriters()
		}(p)
	}
	d.mu.RUnlock()
	wg.Wait()

	// Do NOT save state again — dump.json already has online statuses.

	// Cleanup
	os.Remove(protocol.SocketPath())
	os.Remove(protocol.PIDFilePath())

	slog.Info("daemon stopped for reboot")
	if d.logWriter != nil {
		d.logWriter.Close()
	}
	os.Exit(0)
}

func (d *Daemon) shutdown() {
	slog.Info("daemon shutting down")

	// Stop MCP HTTP server
	if d.mcpServer != nil {
		d.mcpServer.Shutdown()
	}

	// Stop telegraf
	if d.telegraf != nil {
		d.telegraf.Close()
	}

	// Signal goroutines to stop before closing listener so acceptLoop
	// sees stopCh closed and exits without logging spurious errors.
	close(d.stopCh)
	d.listener.Close()

	// Stop all processes in parallel
	d.mu.RLock()
	var wg sync.WaitGroup
	for _, p := range d.processes {
		wg.Add(1)
		go func(proc *Process) {
			defer wg.Done()
			proc.Stop("daemon shutdown")
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
	if d.logWriter != nil {
		d.logWriter.Close()
	}
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

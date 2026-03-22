package daemon

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// monitor watches a running process and handles restarts on exit.
func (d *Daemon) monitor(p *Process) {
	p.mu.Lock()
	monitorPID := p.info.PID
	monitorUptime := p.info.Uptime
	p.mu.Unlock()

	slog.Debug("monitor goroutine started", "name", p.info.Name, "pid", monitorPID)

	exitCode := p.Wait()

	p.mu.Lock()
	wasStopping := p.stopping
	currentPID := p.info.PID
	p.stopping = false
	p.mu.Unlock()

	// Detect stale monitor goroutine: if PID changed, another Start() already happened
	if currentPID != 0 && currentPID != monitorPID {
		slog.Warn("stale monitor goroutine detected, ignoring exit",
			"name", p.info.Name, "monitor_pid", monitorPID, "current_pid", currentPID)
		return
	}

	// Close the exitCh to signal anyone waiting
	close(p.exitCh)

	runDuration := time.Since(monitorUptime)

	if wasStopping {
		p.MarkExited(exitCode, protocol.StatusStopped)
		p.SetReason("stopped by user")
		p.LogAction("process stopped (exit code %d)", exitCode)
		slog.Info("process stopped", "name", p.info.Name, "pid", monitorPID,
			"exit_code", exitCode, "run_duration", runDuration)
		d.autoSave("process stopped")
		return
	}

	p.LogAction("process exited with code %d", exitCode)
	slog.Info("process exited", "name", p.info.Name, "pid", monitorPID,
		"exit_code", exitCode, "run_duration", runDuration)
	d.handleProcessExit(p, exitCode, monitorPID)
}

// handleProcessExit implements the restart logic from the spec.
func (d *Daemon) handleProcessExit(p *Process, exitCode int, exitedPID int) {
	defer d.autoSave("process exit")

	p.mu.Lock()
	policy := p.info.RestartPolicy
	uptime := p.info.Uptime
	restarts := p.info.Restarts
	p.mu.Unlock()

	slog.Debug("handleProcessExit called",
		"name", p.info.Name, "exited_pid", exitedPID, "exit_code", exitCode,
		"restarts", restarts, "auto_restart", policy.AutoRestart,
		"min_uptime", policy.MinUptime.Duration, "max_restarts", policy.MaxRestarts,
		"restart_delay", policy.RestartDelay.Duration)

	// Check restart policy
	if policy.AutoRestart == protocol.RestartNever {
		reason := "autorestart disabled"
		p.MarkExited(exitCode, protocol.StatusStopped)
		p.SetReason(reason)
		p.LogAction("%s, not restarting", reason)
		slog.Info("autorestart=never, marking stopped", "name", p.info.Name, "pid", exitedPID)
		return
	}

	if policy.AutoRestart == protocol.RestartOnFailure && exitCode == 0 {
		reason := "clean exit (autorestart=on-failure)"
		p.MarkExited(exitCode, protocol.StatusStopped)
		p.SetReason(reason)
		p.LogAction("%s, not restarting", reason)
		slog.Info("clean exit with autorestart=on-failure, marking stopped",
			"name", p.info.Name, "pid", exitedPID)
		return
	}

	// Check exit code filters
	if len(policy.NoRestartOnExit) > 0 && containsInt(policy.NoRestartOnExit, exitCode) {
		reason := fmt.Sprintf("exit code %d excluded from restart", exitCode)
		p.MarkExited(exitCode, protocol.StatusStopped)
		p.SetReason(reason)
		p.LogAction("%s", reason)
		slog.Info("exit code in no_restart_on_exit, marking stopped",
			"name", p.info.Name, "pid", exitedPID, "exit_code", exitCode)
		return
	}

	if len(policy.RestartOnExit) > 0 && !containsInt(policy.RestartOnExit, exitCode) {
		reason := fmt.Sprintf("exit code %d not in restart list", exitCode)
		p.MarkExited(exitCode, protocol.StatusErrored)
		p.SetReason(reason)
		p.LogAction("%s", reason)
		slog.Info("exit code not in restart_on_exit, marking errored",
			"name", p.info.Name, "pid", exitedPID, "exit_code", exitCode)
		return
	}

	// Check if process ran long enough to reset counter
	runDuration := time.Since(uptime)
	if runDuration >= policy.MinUptime.Duration {
		p.mu.Lock()
		p.info.Restarts = 0
		restarts = 0
		p.mu.Unlock()
		slog.Info("process ran longer than min_uptime, reset restart counter",
			"name", p.info.Name, "pid", exitedPID,
			"run_duration", runDuration, "min_uptime", policy.MinUptime.Duration)
	} else {
		slog.Info("process exited before min_uptime",
			"name", p.info.Name, "pid", exitedPID,
			"run_duration", runDuration, "min_uptime", policy.MinUptime.Duration,
			"restarts", restarts)
	}

	// Check max restarts
	if policy.MaxRestarts > 0 && restarts >= policy.MaxRestarts {
		reason := fmt.Sprintf("max restarts reached (exit code %d)", exitCode)
		p.MarkExited(exitCode, protocol.StatusErrored)
		p.SetReason(reason)
		p.LogAction("%s — giving up after %d restarts", reason, restarts)
		slog.Info("max restarts reached, marking errored",
			"name", p.info.Name, "pid", exitedPID,
			"restarts", restarts, "max", policy.MaxRestarts)
		return
	}

	// Calculate delay
	delay := policy.RestartDelay.Duration
	if policy.ExpBackoff {
		delay = policy.RestartDelay.Duration << uint(restarts)
		if policy.MaxDelay.Duration > 0 && delay > policy.MaxDelay.Duration {
			delay = policy.MaxDelay.Duration
		}
	}

	maxLabel := "unlimited"
	if policy.MaxRestarts > 0 {
		maxLabel = fmt.Sprintf("%d", policy.MaxRestarts)
	}
	p.LogAction("restarting (attempt %d/%s, delay %s)", restarts+1, maxLabel, delay)
	slog.Info("restarting process",
		"name", p.info.Name, "exited_pid", exitedPID,
		"delay", delay, "restart_count", restarts+1)

	// Mark as stopped temporarily during delay
	p.MarkExited(exitCode, protocol.StatusStopped)

	time.Sleep(delay)

	// Increment restart counter and restart
	p.mu.Lock()
	p.info.Restarts++
	p.mu.Unlock()

	p.CloseLogWriters()
	if err := p.Start(); err != nil {
		reason := fmt.Sprintf("failed to restart: %s", err)
		p.MarkExited(exitCode, protocol.StatusErrored)
		p.SetReason(reason)
		slog.Error("failed to restart process", "name", p.info.Name,
			"exited_pid", exitedPID, "error", err)
		return
	}

	slog.Info("process restarted successfully",
		"name", p.info.Name, "old_pid", exitedPID, "new_pid", p.info.PID)
	p.LogAction("process started (PID %d)", p.info.PID)

	// Monitor the new process instance
	go d.monitor(p)
}

func containsInt(slice []int, val int) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

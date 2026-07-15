package daemon

import (
	"fmt"
	"log/slog"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// monitorInstance watches a specific instance of a process. All identifying
// state (cmd, pid, uptime, instance, exitCh) is captured by the caller and
// passed in explicitly — this avoids scheduling races where the monitor
// goroutine would otherwise read p.info after a concurrent Start() had
// already replaced it.
func (d *Daemon) monitorInstance(p *Process, cmd *exec.Cmd, pid int, uptime time.Time, instance int64, exitCh chan struct{}) {
	slog.Info("monitor goroutine started",
		"name", p.info.Name, "pid", pid, "instance", instance)

	exitCode := waitCmd(cmd)

	slog.Info("monitor: Wait returned",
		"name", p.info.Name, "pid", pid, "instance", instance,
		"exit_code", exitCode)

	p.mu.Lock()
	wasStopping := p.stopping
	stopReason := p.stopReason
	currentInstance := p.instance
	currentPID := p.info.PID
	if instance == currentInstance {
		p.stopping = false
	}
	p.mu.Unlock()

	// Stale check: another Start() has replaced this process instance.
	// Don't touch shared state, don't close exitCh (the new instance has
	// its own), just return. The cmd has already been waited (reaped) above.
	if instance != currentInstance {
		atomic.AddUint64(&d.counters.monitorStales, 1)
		slog.Warn("stale monitor goroutine, ignoring exit (cmd already reaped)",
			"name", p.info.Name,
			"monitor_pid", pid, "monitor_instance", instance,
			"current_pid", currentPID, "current_instance", currentInstance)
		return
	}

	// Close the exitCh to signal anyone waiting in Stop()
	close(exitCh)

	runDuration := time.Since(uptime)

	if wasStopping {
		if stopReason == "" {
			stopReason = "stopped by user"
		}
		p.MarkExited(exitCode, protocol.StatusStopped)
		p.SetReason(stopReason)
		p.LogAction("process stopped: %s (exit code %d)", stopReason, exitCode)
		slog.Info("process stopped", "name", p.info.Name, "pid", pid,
			"instance", instance,
			"exit_code", exitCode, "run_duration", runDuration,
			"reason", stopReason)
		d.autoSave("process stopped")
		return
	}

	p.LogAction("process exited with code %d", exitCode)
	slog.Info("process exited", "name", p.info.Name, "pid", pid,
		"instance", instance,
		"exit_code", exitCode, "run_duration", runDuration)
	d.handleProcessExit(p, exitCode, pid, instance)
}

// monitor starts a monitorInstance goroutine for the current process state.
// It snapshots the state synchronously while holding p.mu so the launched
// goroutine can't race with a concurrent Start().
func (d *Daemon) monitor(p *Process) {
	p.mu.Lock()
	cmd := p.cmd
	pid := p.info.PID
	uptime := p.info.Uptime
	instance := p.instance
	exitCh := p.exitCh
	p.mu.Unlock()
	go d.monitorInstance(p, cmd, pid, uptime, instance, exitCh)
}

// waitCmd blocks on a specific cmd and returns the exit code. Calling Wait
// also reaps the child so its PID is freed.
func waitCmd(cmd *exec.Cmd) int {
	if cmd == nil {
		return -1
	}
	err := cmd.Wait()
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

// handleProcessExit implements the restart logic from the spec. exitedInstance
// is the instance counter value the exited process was tagged with — it is
// used to abort the restart if a concurrent Start() has already replaced the
// process before the restart delay elapses.
func (d *Daemon) handleProcessExit(p *Process, exitCode int, exitedPID int, exitedInstance int64) {
	defer d.autoSave("process exit")

	p.mu.Lock()
	policy := p.info.RestartPolicy
	uptime := p.info.Uptime
	restarts := p.info.Restarts
	cancelCh := p.cancelRestart
	// Record last-exit metadata for telemetry/inspection.
	runMS := time.Since(uptime).Milliseconds()
	p.info.LastExitCode = exitCode
	p.info.LastRunDuration = runMS
	// Any non-zero exit counts as a crash.
	if exitCode != 0 {
		p.crashCount++
	}
	p.mu.Unlock()

	slog.Debug("handleProcessExit called",
		"name", p.info.Name, "exited_pid", exitedPID, "exited_instance", exitedInstance,
		"exit_code", exitCode, "restarts", restarts, "auto_restart", policy.AutoRestart,
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
	slog.Info("supervisor scheduling restart",
		"name", p.info.Name, "exited_pid", exitedPID, "exited_instance", exitedInstance,
		"delay", delay, "restart_count", restarts+1)

	// Mark as stopped temporarily during delay
	p.MarkExited(exitCode, protocol.StatusStopped)
	p.mu.Lock()
	p.info.InRestartDelay = true
	p.info.RestartsSinceReset = restarts
	p.mu.Unlock()

	// Cancellable delay: Stop() closes cancelCh to abort pending restart.
	select {
	case <-time.After(delay):
	case <-cancelCh:
		p.mu.Lock()
		p.info.InRestartDelay = false
		p.mu.Unlock()
		atomic.AddUint64(&d.counters.restartCancels, 1)
		slog.Info("supervisor restart cancelled by Stop()",
			"name", p.info.Name, "exited_pid", exitedPID, "exited_instance", exitedInstance)
		return
	}
	p.mu.Lock()
	p.info.InRestartDelay = false
	p.mu.Unlock()

	// Stale check: a concurrent Start() (user-restart, resurrect) may have
	// already bumped the instance while we were sleeping. If so, abort —
	// there is already a new process and a new monitor.
	currentInstance := p.Instance()
	if currentInstance != exitedInstance {
		slog.Warn("supervisor restart aborted, instance changed during delay",
			"name", p.info.Name,
			"exited_instance", exitedInstance, "current_instance", currentInstance)
		return
	}

	// Increment restart counter and restart
	p.mu.Lock()
	p.info.Restarts++
	p.mu.Unlock()

	p.CloseLogWriters()
	if err := p.Start("supervisor-restart"); err != nil {
		reason := fmt.Sprintf("failed to restart: %s", err)
		p.MarkExited(exitCode, protocol.StatusErrored)
		p.SetReason(reason)
		slog.Error("failed to restart process", "name", p.info.Name,
			"exited_pid", exitedPID, "error", err)
		return
	}

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

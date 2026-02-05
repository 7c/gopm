package daemon

import (
	"log/slog"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// monitor watches a running process and handles restarts on exit.
func (d *Daemon) monitor(p *Process) {
	exitCode := p.Wait()

	p.mu.Lock()
	wasStopping := p.stopping
	p.stopping = false
	p.mu.Unlock()

	// Close the exitCh to signal anyone waiting
	close(p.exitCh)

	if wasStopping {
		p.MarkExited(exitCode, protocol.StatusStopped)
		slog.Info("process stopped", "name", p.info.Name, "exit_code", exitCode)
		return
	}

	slog.Info("process exited", "name", p.info.Name, "exit_code", exitCode)
	d.handleProcessExit(p, exitCode)
}

// handleProcessExit implements the restart logic from the spec.
func (d *Daemon) handleProcessExit(p *Process, exitCode int) {
	p.mu.Lock()
	policy := p.info.RestartPolicy
	uptime := p.info.Uptime
	restarts := p.info.Restarts
	p.mu.Unlock()

	// Check restart policy
	if policy.AutoRestart == protocol.RestartNever {
		p.MarkExited(exitCode, protocol.StatusStopped)
		slog.Info("autorestart=never, marking stopped", "name", p.info.Name)
		return
	}

	if policy.AutoRestart == protocol.RestartOnFailure && exitCode == 0 {
		p.MarkExited(exitCode, protocol.StatusStopped)
		slog.Info("clean exit with autorestart=on-failure, marking stopped", "name", p.info.Name)
		return
	}

	// Check exit code filters
	if len(policy.NoRestartOnExit) > 0 && containsInt(policy.NoRestartOnExit, exitCode) {
		p.MarkExited(exitCode, protocol.StatusStopped)
		slog.Info("exit code in no_restart_on_exit, marking stopped",
			"name", p.info.Name, "exit_code", exitCode)
		return
	}

	if len(policy.RestartOnExit) > 0 && !containsInt(policy.RestartOnExit, exitCode) {
		p.MarkExited(exitCode, protocol.StatusErrored)
		slog.Info("exit code not in restart_on_exit, marking errored",
			"name", p.info.Name, "exit_code", exitCode)
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
			"name", p.info.Name, "run_duration", runDuration)
	}

	// Check max restarts
	if policy.MaxRestarts > 0 && restarts >= policy.MaxRestarts {
		p.MarkExited(exitCode, protocol.StatusErrored)
		slog.Info("max restarts reached, marking errored",
			"name", p.info.Name, "restarts", restarts, "max", policy.MaxRestarts)
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

	slog.Info("restarting process",
		"name", p.info.Name, "delay", delay, "restart_count", restarts+1)

	// Mark as stopped temporarily during delay
	p.MarkExited(exitCode, protocol.StatusStopped)

	time.Sleep(delay)

	// Increment restart counter and restart
	p.mu.Lock()
	p.info.Restarts++
	p.mu.Unlock()

	p.CloseLogWriters()
	if err := p.Start(); err != nil {
		p.MarkExited(exitCode, protocol.StatusErrored)
		slog.Error("failed to restart process", "name", p.info.Name, "error", err)
		return
	}

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

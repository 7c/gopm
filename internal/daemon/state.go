package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// stateSaveLogMinInterval throttles the "state saved" Debug log when the
// process table has not changed. A flapping process that hits autoSave every
// ~2s would otherwise emit millions of lines a day even at Debug level.
const stateSaveLogMinInterval = 30 * time.Second

// autoSave is a convenience wrapper that logs errors from SaveState.
func (d *Daemon) autoSave(reason string) {
	if err := d.SaveState(); err != nil {
		atomic.AddUint64(&d.counters.stateSaveFailures, 1)
		slog.Error("auto-save failed", "reason", reason, "error", err)
	}
}

// SaveState persists the current process table to dump.json.
func (d *Daemon) SaveState() error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var infos []protocol.ProcessInfo
	for _, p := range d.processes {
		infos = append(infos, p.Info())
	}

	data, err := json.MarshalIndent(infos, "", "  ")
	if err != nil {
		atomic.AddUint64(&d.counters.stateSaveFailures, 1)
		return fmt.Errorf("marshal state: %w", err)
	}

	path := protocol.DumpFilePath()
	if err := os.WriteFile(path, data, 0644); err != nil {
		atomic.AddUint64(&d.counters.stateSaveFailures, 1)
		return fmt.Errorf("write dump file: %w", err)
	}

	atomic.AddUint64(&d.counters.stateSaves, 1)
	if d.shouldLogStateSave(len(infos)) {
		slog.Debug("state saved", "path", path, "count", len(infos))
	}
	return nil
}

// shouldLogStateSave returns true when the "state saved" debug line is worth
// emitting: the process count changed since the last logged save, or enough
// time has passed. This prevents crash-looping supervisors from generating
// millions of identical log lines.
func (d *Daemon) shouldLogStateSave(count int) bool {
	d.stateSaveLogMu.Lock()
	defer d.stateSaveLogMu.Unlock()
	now := time.Now()
	if count != d.stateSaveLogSeen || now.Sub(d.stateSaveLogLast) >= stateSaveLogMinInterval {
		d.stateSaveLogLast = now
		d.stateSaveLogSeen = count
		return true
	}
	return false
}

// LoadState reads the dump.json and returns process infos.
func LoadState() ([]protocol.ProcessInfo, error) {
	path := protocol.DumpFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dump file: %w", err)
	}

	var infos []protocol.ProcessInfo
	if err := json.Unmarshal(data, &infos); err != nil {
		return nil, fmt.Errorf("invalid dump file: %w", err)
	}

	return infos, nil
}

// shouldResurrectAsRunning reports whether a saved ProcessInfo should be
// started on resurrect rather than merely registered.
//
// A saved status of "online" is the obvious case. A saved
// InRestartDelay=true is the subtler case: the supervisor had already
// decided to restart the process when state was captured (typically because
// a `gopm reboot` fired inside the restart-delay window between an exit and
// the scheduled restart). Without this, such a process is persisted as
// "stopped", resurrect skips it, and it stays orphaned until a manual
// `gopm start` — even though autorestart is "always" and the user never
// asked for it to stop.
func shouldResurrectAsRunning(info protocol.ProcessInfo) bool {
	if info.Status == protocol.StatusOnline {
		return true
	}
	return info.InRestartDelay
}

// ResurrectProcesses restores all processes from dump.json.
// Online (or mid-restart-delay) processes are started; other stopped/errored
// entries are registered without starting.
func (d *Daemon) ResurrectProcesses() ([]protocol.ProcessInfo, error) {
	atomic.AddUint64(&d.counters.resurrectCount, 1)
	infos, err := LoadState()
	if err != nil {
		return nil, err
	}

	var resurrected []protocol.ProcessInfo
	for _, info := range infos {
		if shouldResurrectAsRunning(info) {
			reason := "resurrect"
			if info.Status != protocol.StatusOnline {
				reason = "resurrect-restart-delay"
				slog.Info("resurrecting process caught mid restart-delay",
					"name", info.Name, "saved_status", info.Status)
			}
			params := infoToStartParams(info)
			proc, err := d.startProcessWithReason(params, reason)
			if err != nil {
				slog.Error("failed to resurrect process", "name", info.Name, "error", err)
				// Register as errored so the process remains visible and
				// can be restarted manually or investigated.
				d.mu.Lock()
				if _, exists := d.processes[info.Name]; exists {
					d.mu.Unlock()
					continue
				}
				id := d.nextID
				d.nextID++
				d.mu.Unlock()

				proc := &Process{info: info}
				proc.info.ID = id
				proc.info.PID = 0
				proc.info.Status = protocol.StatusErrored
				proc.info.StatusReason = err.Error()
				proc.info.CPU = 0
				proc.info.Memory = 0

				d.mu.Lock()
				d.processes[info.Name] = proc
				d.mu.Unlock()

				slog.Info("registered failed process as errored", "name", info.Name)
				resurrected = append(resurrected, proc.Info())
				continue
			}
			resurrected = append(resurrected, proc.Info())
		} else {
			// Register stopped/errored process without starting it
			d.mu.Lock()
			if _, exists := d.processes[info.Name]; exists {
				d.mu.Unlock()
				continue
			}
			id := d.nextID
			d.nextID++
			d.mu.Unlock()

			proc := &Process{
				info: info,
			}
			proc.info.ID = id
			proc.info.PID = 0

			d.mu.Lock()
			d.processes[info.Name] = proc
			d.mu.Unlock()

			slog.Info("registered saved process", "name", info.Name, "status", info.Status)
			resurrected = append(resurrected, proc.Info())
		}
	}

	return resurrected, nil
}

// infoToStartParams converts a ProcessInfo to StartParams for resurrection.
func infoToStartParams(info protocol.ProcessInfo) protocol.StartParams {
	params := protocol.StartParams{
		Command:     info.Command,
		Name:        info.Name,
		Args:        info.Args,
		Cwd:         info.Cwd,
		Env:         info.Env,
		Interpreter: info.Interpreter,
		AutoRestart: string(info.RestartPolicy.AutoRestart),
		LogOut:      info.LogOut,
		LogErr:      info.LogErr,
	}

	maxRestarts := info.RestartPolicy.MaxRestarts
	params.MaxRestarts = &maxRestarts

	if info.RestartPolicy.MinUptime.Duration > 0 {
		params.MinUptime = info.RestartPolicy.MinUptime.String()
	}
	if info.RestartPolicy.RestartDelay.Duration > 0 {
		params.RestartDelay = info.RestartPolicy.RestartDelay.String()
	}
	params.ExpBackoff = info.RestartPolicy.ExpBackoff
	if info.RestartPolicy.MaxDelay.Duration > 0 {
		params.MaxDelay = info.RestartPolicy.MaxDelay.String()
	}
	if info.RestartPolicy.KillTimeout.Duration > 0 {
		params.KillTimeout = info.RestartPolicy.KillTimeout.String()
	}
	if info.MaxLogSize > 0 {
		params.MaxLogSize = fmt.Sprintf("%d", info.MaxLogSize)
	}

	return params
}

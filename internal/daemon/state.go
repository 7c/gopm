package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/7c/gopm/internal/protocol"
)

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
		return fmt.Errorf("marshal state: %w", err)
	}

	path := protocol.DumpFilePath()
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write dump file: %w", err)
	}

	slog.Info("state saved", "path", path, "count", len(infos))
	return nil
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

// ResurrectProcesses starts all previously-online processes from dump.json.
func (d *Daemon) ResurrectProcesses() ([]protocol.ProcessInfo, error) {
	infos, err := LoadState()
	if err != nil {
		return nil, err
	}

	var resurrected []protocol.ProcessInfo
	for _, info := range infos {
		if info.Status != protocol.StatusOnline {
			continue
		}

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

		proc, err := d.startProcess(params)
		if err != nil {
			slog.Error("failed to resurrect process", "name", info.Name, "error", err)
			continue
		}
		resurrected = append(resurrected, proc.Info())
	}

	return resurrected, nil
}

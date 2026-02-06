package daemon

import (
	"log/slog"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

const listenersInterval = 60 * time.Second

// scanListeners periodically scans listening ports for all online processes.
func (d *Daemon) scanListeners() {
	// Run an initial scan immediately.
	d.doListenerScan()

	ticker := time.NewTicker(listenersInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.doListenerScan()
		case <-d.stopCh:
			return
		}
	}
}

func (d *Daemon) doListenerScan() {
	d.mu.RLock()
	procs := make([]*Process, 0, len(d.processes))
	for _, p := range d.processes {
		procs = append(procs, p)
	}
	d.mu.RUnlock()

	for _, p := range procs {
		p.mu.Lock()
		if p.info.Status != protocol.StatusOnline || p.info.PID == 0 {
			p.mu.Unlock()
			continue
		}
		pid := p.info.PID
		p.mu.Unlock()

		listeners := scanProcessListeners(pid)

		p.mu.Lock()
		p.info.Listeners = listeners
		p.mu.Unlock()
	}

	slog.Debug("listener scan complete")
}

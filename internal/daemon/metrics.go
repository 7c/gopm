package daemon

import (
	"log/slog"
	"syscall"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

const (
	metricsInterval = 2 * time.Second
	clockTicksPerSec = 100 // standard on most Linux systems
)

// sampleMetrics periodically samples CPU and memory for all online processes.
func (d *Daemon) sampleMetrics() {
	ticker := time.NewTicker(metricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
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

				// Check if process still exists
				if !processExists(pid) {
					slog.Warn("process disappeared", "name", p.info.Name, "pid", pid)
					// Send SIGKILL to the process group to ensure cmd.Wait()
					// returns in monitor(), which triggers restart logic.
					syscall.Kill(-pid, syscall.SIGKILL)
					continue
				}

				rss, cpuTicks, err := sampleProcessMetrics(pid)
				if err != nil {
					continue
				}

				p.mu.Lock()
				p.info.Memory = rss

				// CPU calculation
				now := time.Now()
				elapsed := now.Sub(p.lastSample).Seconds()
				if elapsed > 0 && p.lastTicks > 0 {
					deltaTicks := cpuTicks - p.lastTicks
					p.info.CPU = float64(deltaTicks) / elapsed / clockTicksPerSec * 100
					if p.info.CPU < 0 {
						p.info.CPU = 0
					}
				}
				p.lastTicks = cpuTicks
				p.lastSample = now
				p.mu.Unlock()
			}

			// Emit telegraf metrics
			if d.telegraf != nil {
				d.mu.RLock()
				var infos []protocol.ProcessInfo
				for _, proc := range d.processes {
					infos = append(infos, proc.Info())
				}
				d.mu.RUnlock()
				d.telegraf.Emit(infos, time.Since(d.startTime))
			}

		case <-d.stopCh:
			return
		}
	}
}

package daemon

import (
	"log/slog"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/7c/gopm/internal/protocol"
	"github.com/7c/gopm/internal/telemetry"
)

const (
	metricsInterval = 2 * time.Second
	clockTicksPerSec = 100 // standard on most Linux systems
)

// sampleMetrics periodically samples CPU and memory for all online processes.
func (d *Daemon) sampleMetrics() {
	ticker := time.NewTicker(metricsInterval)
	defer ticker.Stop()

	snapshotTick := 0

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

				childCount := countDescendants(pid)

				// Snapshot log stats before taking p.mu (writers have their own lock).
				var logBytes int64
				var logRotations int
				if p.stdout != nil {
					b, r := p.stdout.Underlying().Stats()
					logBytes += b
					logRotations += r
				}
				if p.stderr != nil {
					b, r := p.stderr.Underlying().Stats()
					logBytes += b
					logRotations += r
				}

				p.mu.Lock()
				p.info.Memory = rss
				if rss > p.memoryPeak {
					p.memoryPeak = rss
				}
				p.info.ChildCount = childCount
				p.logBytesWritten = logBytes
				p.logRotations = logRotations

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
				// Snapshot RPC call map (guarded by d.mu).
				rpcCallsCopy := make(map[string]uint64, len(d.counters.rpcCallsByMethod))
				for k, v := range d.counters.rpcCallsByMethod {
					rpcCallsCopy[k] = v
				}
				d.mu.RUnlock()
				dc := telemetry.DaemonCounters{
					RPCCallsByMethod:  rpcCallsCopy,
					RPCErrors:         atomic.LoadUint64(&d.counters.rpcErrors),
					StateSaves:        atomic.LoadUint64(&d.counters.stateSaves),
					StateSaveFailures: atomic.LoadUint64(&d.counters.stateSaveFailures),
					ResurrectCount:    atomic.LoadUint64(&d.counters.resurrectCount),
					ZombieDetections:  ZombieDetections(),
					MonitorStales:     atomic.LoadUint64(&d.counters.monitorStales),
					RestartCancels:    atomic.LoadUint64(&d.counters.restartCancels),
				}
				d.telegraf.Emit(infos, time.Since(d.startTime), dc)
			}

			// Capture time-series snapshots every snapshotInterval ticks (60s).
			snapshotTick++
			if snapshotTick >= snapshotInterval {
				snapshotTick = 0
				d.captureSnapshots()
			}

		case <-d.stopCh:
			return
		}
	}
}

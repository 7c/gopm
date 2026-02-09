package daemon

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

const (
	// maxSnapshots is the ring buffer capacity: 18 hours × 60 snapshots/hour.
	maxSnapshots = 1080
	// snapshotInterval is the number of metrics ticks between captures.
	// metrics tick = 2s, so 30 ticks = 60 seconds.
	snapshotInterval = 30
)

// snapshotRing is a fixed-capacity circular buffer of MetricsSnapshot.
type snapshotRing struct {
	buf   [maxSnapshots]protocol.MetricsSnapshot
	head  int // next write position
	count int // number of valid entries (0..maxSnapshots)
}

// push appends a snapshot, overwriting the oldest if full.
func (r *snapshotRing) push(s protocol.MetricsSnapshot) {
	r.buf[r.head] = s
	r.head = (r.head + 1) % maxSnapshots
	if r.count < maxSnapshots {
		r.count++
	}
}

// slice returns snapshots in chronological order, filtered to the last N hours.
// If hours <= 0, all snapshots are returned.
func (r *snapshotRing) slice(hours int) []protocol.MetricsSnapshot {
	if r.count == 0 {
		return nil
	}

	start := 0
	if r.count == maxSnapshots {
		start = r.head // head points to the oldest when full
	}

	result := make([]protocol.MetricsSnapshot, r.count)
	for i := 0; i < r.count; i++ {
		result[i] = r.buf[(start+i)%maxSnapshots]
	}

	if hours > 0 {
		cutoff := time.Now().Unix() - int64(hours*3600)
		trimIdx := 0
		for trimIdx < len(result) && result[trimIdx].Timestamp < cutoff {
			trimIdx++
		}
		result = result[trimIdx:]
	}

	return result
}

// captureSnapshots records a MetricsSnapshot for every known process.
func (d *Daemon) captureSnapshots() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now().Unix()

	for name, p := range d.processes {
		p.mu.Lock()
		snap := protocol.MetricsSnapshot{
			Timestamp: now,
			CPU:       p.info.CPU,
			Memory:    p.info.Memory,
			Restarts:  p.info.Restarts,
			Status:    p.info.Status,
		}
		if p.info.Status == protocol.StatusOnline && !p.info.Uptime.IsZero() {
			snap.UptimeSec = now - p.info.Uptime.Unix()
		}
		p.mu.Unlock()

		ring, ok := d.snapshots[name]
		if !ok {
			ring = &snapshotRing{}
			d.snapshots[name] = ring
		}
		ring.push(snap)
	}
}

// handleStats returns snapshot history for the requested target.
func (d *Daemon) handleStats(params json.RawMessage) protocol.Response {
	var sp protocol.StatsParams
	if err := json.Unmarshal(params, &sp); err != nil {
		return errorResponse("invalid stats params: " + err.Error())
	}
	if sp.Target == "" {
		sp.Target = "all"
	}
	if sp.Hours <= 0 {
		sp.Hours = 6
	}
	if sp.Hours > 18 {
		sp.Hours = 18
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make(protocol.StatsResult)

	if sp.Target == "all" {
		for name, ring := range d.snapshots {
			if snaps := ring.slice(sp.Hours); len(snaps) > 0 {
				result[name] = snaps
			}
		}
	} else {
		// Resolve by name first, then by ID. Inline to avoid calling
		// resolveTarget() which acquires its own d.mu.RLock().
		names := d.resolveSnapshotNames(sp.Target)
		for _, name := range names {
			if ring, ok := d.snapshots[name]; ok {
				if snaps := ring.slice(sp.Hours); len(snaps) > 0 {
					result[name] = snaps
				}
			}
		}
	}

	return successResponse(result)
}

// resolveSnapshotNames returns process names matching the target.
// Must be called with d.mu held (read or write).
func (d *Daemon) resolveSnapshotNames(target string) []string {
	// By name
	if _, ok := d.processes[target]; ok {
		return []string{target}
	}
	// By ID
	if id, err := strconv.Atoi(target); err == nil {
		for _, p := range d.processes {
			if p.info.ID == id {
				return []string{p.info.Name}
			}
		}
	}
	return nil
}

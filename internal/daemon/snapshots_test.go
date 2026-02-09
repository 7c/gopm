package daemon

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

func TestSnapshotRingPush(t *testing.T) {
	var r snapshotRing
	for i := 0; i < 5; i++ {
		r.push(protocol.MetricsSnapshot{
			Timestamp: int64(i),
			CPU:       float64(i),
		})
	}
	if r.count != 5 {
		t.Fatalf("count = %d, want 5", r.count)
	}
	snaps := r.slice(0)
	if len(snaps) != 5 {
		t.Fatalf("len = %d, want 5", len(snaps))
	}
	for i, s := range snaps {
		if s.Timestamp != int64(i) {
			t.Errorf("snap[%d].Timestamp = %d, want %d", i, s.Timestamp, i)
		}
	}
}

func TestSnapshotRingWrapAround(t *testing.T) {
	var r snapshotRing
	total := maxSnapshots + 100
	for i := 0; i < total; i++ {
		r.push(protocol.MetricsSnapshot{Timestamp: int64(i)})
	}
	if r.count != maxSnapshots {
		t.Fatalf("count = %d, want %d", r.count, maxSnapshots)
	}
	snaps := r.slice(0)
	if len(snaps) != maxSnapshots {
		t.Fatalf("len = %d, want %d", len(snaps), maxSnapshots)
	}
	// Oldest should be 100, newest should be total-1.
	if snaps[0].Timestamp != 100 {
		t.Errorf("oldest = %d, want 100", snaps[0].Timestamp)
	}
	if snaps[len(snaps)-1].Timestamp != int64(total-1) {
		t.Errorf("newest = %d, want %d", snaps[len(snaps)-1].Timestamp, total-1)
	}
	// Verify chronological order.
	for i := 1; i < len(snaps); i++ {
		if snaps[i].Timestamp <= snaps[i-1].Timestamp {
			t.Fatalf("not sorted at index %d: %d <= %d", i, snaps[i].Timestamp, snaps[i-1].Timestamp)
		}
	}
}

func TestSnapshotRingSliceHours(t *testing.T) {
	var r snapshotRing
	now := time.Now().Unix()
	// 10 hours of data, one point per minute = 600 points.
	for i := 0; i < 600; i++ {
		ts := now - int64((600-i)*60)
		r.push(protocol.MetricsSnapshot{Timestamp: ts, CPU: float64(i)})
	}

	snaps := r.slice(2)
	cutoff := now - 2*3600
	for _, s := range snaps {
		if s.Timestamp < cutoff {
			t.Errorf("snapshot ts %d before cutoff %d", s.Timestamp, cutoff)
		}
	}
	// ~120 points for 2 hours.
	if len(snaps) < 110 || len(snaps) > 130 {
		t.Errorf("expected ~120 snapshots for 2h, got %d", len(snaps))
	}
}

func TestSnapshotRingEmpty(t *testing.T) {
	var r snapshotRing
	snaps := r.slice(0)
	if snaps != nil {
		t.Errorf("expected nil, got %d snapshots", len(snaps))
	}
	snaps = r.slice(6)
	if snaps != nil {
		t.Errorf("expected nil for hours filter on empty ring, got %d", len(snaps))
	}
}

func TestHandleStatsEmpty(t *testing.T) {
	d := &Daemon{
		processes: make(map[string]*Process),
		snapshots: make(map[string]*snapshotRing),
	}
	params, _ := json.Marshal(protocol.StatsParams{Target: "all", Hours: 6})
	resp := d.handleStats(params)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}
	var result protocol.StatsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}
}

func TestHandleStatsWithData(t *testing.T) {
	d := &Daemon{
		processes: make(map[string]*Process),
		snapshots: make(map[string]*snapshotRing),
	}

	// Add a process.
	d.processes["api"] = &Process{
		info: protocol.ProcessInfo{ID: 0, Name: "api"},
	}

	// Push some snapshots.
	ring := &snapshotRing{}
	now := time.Now().Unix()
	for i := 0; i < 10; i++ {
		ring.push(protocol.MetricsSnapshot{
			Timestamp: now - int64((10-i)*60),
			CPU:       float64(i),
			Memory:    uint64(i * 1024),
		})
	}
	d.snapshots["api"] = ring

	// Query all.
	params, _ := json.Marshal(protocol.StatsParams{Target: "all", Hours: 1})
	resp := d.handleStats(params)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}
	var result protocol.StatsResult
	json.Unmarshal(resp.Data, &result)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if _, ok := result["api"]; !ok {
		t.Error("expected 'api' in result")
	}
	if len(result["api"]) != 10 {
		t.Errorf("expected 10 snapshots, got %d", len(result["api"]))
	}
}

func TestHandleStatsTargetByName(t *testing.T) {
	d := &Daemon{
		processes: make(map[string]*Process),
		snapshots: make(map[string]*snapshotRing),
	}
	d.processes["api"] = &Process{info: protocol.ProcessInfo{ID: 0, Name: "api"}}
	d.processes["worker"] = &Process{info: protocol.ProcessInfo{ID: 1, Name: "worker"}}

	now := time.Now().Unix()
	for _, name := range []string{"api", "worker"} {
		ring := &snapshotRing{}
		ring.push(protocol.MetricsSnapshot{Timestamp: now, CPU: 5.0})
		d.snapshots[name] = ring
	}

	params, _ := json.Marshal(protocol.StatsParams{Target: "api", Hours: 6})
	resp := d.handleStats(params)
	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Error)
	}
	var result protocol.StatsResult
	json.Unmarshal(resp.Data, &result)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if _, ok := result["api"]; !ok {
		t.Error("expected 'api' in result")
	}
}

func TestHandleStatsTargetByID(t *testing.T) {
	d := &Daemon{
		processes: make(map[string]*Process),
		snapshots: make(map[string]*snapshotRing),
	}
	d.processes["worker"] = &Process{info: protocol.ProcessInfo{ID: 1, Name: "worker"}}

	ring := &snapshotRing{}
	ring.push(protocol.MetricsSnapshot{Timestamp: time.Now().Unix(), CPU: 3.0})
	d.snapshots["worker"] = ring

	params, _ := json.Marshal(protocol.StatsParams{Target: "1", Hours: 6})
	resp := d.handleStats(params)
	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Error)
	}
	var result protocol.StatsResult
	json.Unmarshal(resp.Data, &result)
	if _, ok := result["worker"]; !ok {
		t.Error("expected 'worker' in result when querying by ID")
	}
}

func TestHandleStatsHoursClamp(t *testing.T) {
	d := &Daemon{
		processes: make(map[string]*Process),
		snapshots: make(map[string]*snapshotRing),
	}

	// Hours 0 should default to 6, hours 99 should clamp to 18.
	params, _ := json.Marshal(protocol.StatsParams{Target: "all", Hours: 0})
	resp := d.handleStats(params)
	if !resp.Success {
		t.Fatalf("hours=0: expected success, got: %s", resp.Error)
	}

	params, _ = json.Marshal(protocol.StatsParams{Target: "all", Hours: 99})
	resp = d.handleStats(params)
	if !resp.Success {
		t.Fatalf("hours=99: expected success, got: %s", resp.Error)
	}
}

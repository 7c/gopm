package telemetry

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// newTestEmitter spins up a local UDP listener and an emitter pointed at it.
// The caller must Close the listener.
func newTestEmitter(t *testing.T) (*TelegrafEmitter, *net.UDPConn) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	listener, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	emitter, err := NewTelegrafEmitter(listener.LocalAddr().(*net.UDPAddr), "gopm")
	if err != nil {
		listener.Close()
		t.Fatalf("emitter: %v", err)
	}
	return emitter, listener
}

// recv reads one UDP packet from listener with a short deadline and returns
// the payload as a string.
func recv(t *testing.T, listener *net.UDPConn) string {
	t.Helper()
	listener.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64*1024)
	n, _, err := listener.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(buf[:n])
}

func sampleProcessInfo() protocol.ProcessInfo {
	return protocol.ProcessInfo{
		ID:                     7,
		Name:                   "fwbackend3",
		Status:                 protocol.StatusOnline,
		PID:                    42,
		Restarts:               3,
		Uptime:                 time.Now().Add(-30 * time.Second),
		Memory:                 100 * 1024 * 1024,
		CPU:                    12.5,
		Listeners:              []string{"tcp/8080", "tcp/9090"},
		StartCount:             5,
		StopCount:              2,
		CrashCount:             4,
		UserRestartCount:       1,
		SupervisorRestartCount: 4,
		Instance:               5,
		LastExitCode:           1,
		LastRunDuration:        12345,
		InRestartDelay:         false,
		RestartsSinceReset:     2,
		MemoryPeak:             150 * 1024 * 1024,
		ChildCount:             18,
		LogBytesWritten:        987654,
		LogRotations:           3,
	}
}

// TestEmit_ProcessLineHasAllLifecycleFields verifies the per-process line
// includes every new lifecycle metric.
func TestEmit_ProcessLineHasAllLifecycleFields(t *testing.T) {
	em, listener := newTestEmitter(t)
	defer em.Close()
	defer listener.Close()

	info := sampleProcessInfo()
	em.Emit([]protocol.ProcessInfo{info}, 5*time.Minute, DaemonCounters{
		RPCCallsByMethod: map[string]uint64{},
	})

	payload := recv(t, listener)

	wanted := []string{
		"gopm,name=fwbackend3,id=7,status=online",
		"pid=42i",
		"memory=104857600i",
		"memory_peak=157286400i",
		"child_count=18i",
		"start_count=5i",
		"stop_count=2i",
		"crash_count=4i",
		"user_restart_count=1i",
		"supervisor_restart_count=4i",
		"instance=5i",
		"last_exit_code=1i",
		"last_run_duration_ms=12345i",
		"restarts_since_reset=2i",
		"in_restart_delay=false",
		"log_bytes_written=987654i",
		"log_rotations=3i",
		"listener_count=2i",
	}
	for _, w := range wanted {
		if !strings.Contains(payload, w) {
			t.Errorf("payload missing %q\npayload:\n%s", w, payload)
		}
	}
}

// TestEmit_DaemonSummaryHasCounters verifies the gopm_daemon line carries
// every daemon-wide counter.
func TestEmit_DaemonSummaryHasCounters(t *testing.T) {
	em, listener := newTestEmitter(t)
	defer em.Close()
	defer listener.Close()

	dc := DaemonCounters{
		RPCCallsByMethod: map[string]uint64{
			"start":   3,
			"restart": 7,
		},
		RPCErrors:         2,
		StateSaves:        10,
		StateSaveFailures: 1,
		ResurrectCount:    4,
		ZombieDetections:  5,
		MonitorStales:     6,
		RestartCancels:    8,
	}
	em.Emit([]protocol.ProcessInfo{sampleProcessInfo()}, 60*time.Second, dc)

	payload := recv(t, listener)

	wanted := []string{
		"gopm_daemon,host=",
		"processes_total=1i",
		"total_children=18i",
		"rpc_errors=2i",
		"state_saves=10i",
		"state_save_failures=1i",
		"resurrect_count=4i",
		"zombie_detections=5i",
		"monitor_stales=6i",
		"restart_cancels=8i",
		// Per-method RPC series
		"gopm_rpc,host=",
		"method=start calls=3i",
		"method=restart calls=7i",
	}
	for _, w := range wanted {
		if !strings.Contains(payload, w) {
			t.Errorf("daemon payload missing %q\npayload:\n%s", w, payload)
		}
	}
}

// TestEmit_OfflineProcessOmitsLivenessFields ensures offline/errored processes
// emit lifecycle counters but not the live-only fields (cpu/memory/uptime).
func TestEmit_OfflineProcessOmitsLiveFields(t *testing.T) {
	em, listener := newTestEmitter(t)
	defer em.Close()
	defer listener.Close()

	info := sampleProcessInfo()
	info.Status = protocol.StatusErrored
	info.PID = 0
	info.Memory = 0
	info.CPU = 0
	em.Emit([]protocol.ProcessInfo{info}, 0, DaemonCounters{
		RPCCallsByMethod: map[string]uint64{},
	})

	payload := recv(t, listener)

	// Lifecycle counters still present.
	if !strings.Contains(payload, "crash_count=4i") {
		t.Errorf("offline payload missing crash_count\npayload:\n%s", payload)
	}
	// Live-only fields must NOT appear on the offline line.
	if strings.Contains(payload, "pid=0i,cpu=") {
		t.Errorf("offline payload should not carry pid/cpu fields\npayload:\n%s", payload)
	}
}

package daemon

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// newTestDaemon returns a minimal Daemon suitable for race/restart tests.
func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	t.Setenv("GOPM_HOME", t.TempDir())
	os.MkdirAll(protocol.GopmHome(), 0755)
	return &Daemon{
		processes: make(map[string]*Process),
		snapshots: make(map[string]*snapshotRing),
		startTime: time.Now(),
		stopCh:    make(chan struct{}),
		home:      protocol.GopmHome(),
		counters: daemonCounters{
			rpcCallsByMethod: make(map[string]uint64),
		},
	}
}

// processAlive returns true iff a process with the given PID is alive.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// waitUntil polls fn until it returns true or the deadline expires.
func waitUntil(timeout time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fn()
}

// startLongRunning creates and starts a Process that runs /bin/sleep 30.
// A monitor goroutine is spawned so Stop() can complete.
func startLongRunning(t *testing.T, d *Daemon, name string) *Process {
	t.Helper()
	tmp := t.TempDir()
	params := protocol.StartParams{
		Command:     "/bin/sleep",
		Args:        []string{"30"},
		Name:        name,
		Cwd:         tmp,
		LogOut:      filepath.Join(tmp, name+"-out.log"),
		LogErr:      filepath.Join(tmp, name+"-err.log"),
		AutoRestart: "no",
	}
	maxR := 0
	params.MaxRestarts = &maxR
	p, err := d.startProcess(params)
	if err != nil {
		t.Fatalf("startProcess: %v", err)
	}
	return p
}

// TestStartBumpsInstanceCounter verifies each Start() increments instance.
func TestStartBumpsInstanceCounter(t *testing.T) {
	d := newTestDaemon(t)
	p := startLongRunning(t, d, "inst")

	if got := p.Instance(); got != 1 {
		t.Fatalf("instance after 1st Start = %d, want 1", got)
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := p.Start("test-2"); err != nil {
		t.Fatalf("Start #2: %v", err)
	}
	go d.monitor(p)
	if got := p.Instance(); got != 2 {
		t.Errorf("instance after 2nd Start = %d, want 2", got)
	}
	p.Stop()
}

// TestStopThenStartReapsOldProcess verifies the normal Stop()+Start() flow:
// after Stop() returns, the old cmd has been reaped by its monitor, and a
// subsequent Start() does NOT trigger the zombie-detection safety net.
func TestStopThenStartReapsOldProcess(t *testing.T) {
	d := newTestDaemon(t)
	p := startLongRunning(t, d, "stoprestart")

	firstPID := p.info.PID
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// After Stop(), the monitor has called cmd.Wait() and closed exitCh.
	// p.cmd.ProcessState must be non-nil (reaped).
	p.mu.Lock()
	reaped := p.cmd != nil && p.cmd.ProcessState != nil
	p.mu.Unlock()
	if !reaped {
		t.Errorf("cmd not reaped after Stop() — ProcessState is nil")
	}

	// Old PID must be dead.
	if !waitUntil(1*time.Second, func() bool { return !processAlive(firstPID) }) {
		t.Errorf("firstPID %d still alive after Stop", firstPID)
	}

	// Subsequent Start() is clean — zombie branch must not fire.
	if err := p.Start("second"); err != nil {
		t.Fatalf("Start #2: %v", err)
	}
	go d.monitor(p)
	secondPID := p.info.PID
	if secondPID == firstPID {
		t.Error("second Start got the same PID as the first")
	}
	if !processAlive(secondPID) {
		t.Errorf("second process %d not alive", secondPID)
	}

	p.Stop()
}

// TestStopClosesCancelRestart verifies Stop() closes the cancelRestart
// channel so any waiting supervisor observes the cancel signal.
func TestStopClosesCancelRestart(t *testing.T) {
	d := newTestDaemon(t)
	p := startLongRunning(t, d, "cancel")

	cancelCh := p.cancelRestart
	if cancelCh == nil {
		t.Fatal("cancelRestart is nil after Start")
	}

	stopDone := make(chan struct{})
	go func() {
		p.Stop()
		close(stopDone)
	}()

	select {
	case <-cancelCh:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelRestart not closed within 3s")
	}
	<-stopDone
}

// TestStopCancelsPendingSupervisorRestart verifies the end-to-end flow:
// a process crashes, the supervisor enters its restart delay, then Stop()
// is called. The supervisor must abort the pending restart — no new
// Start() must occur.
func TestStopCancelsPendingSupervisorRestart(t *testing.T) {
	d := newTestDaemon(t)

	tmp := t.TempDir()
	params := protocol.StartParams{
		Command:      "/bin/sh",
		Args:         []string{"-c", "exit 1"},
		Name:         "cancelme",
		Cwd:          tmp,
		LogOut:       filepath.Join(tmp, "out.log"),
		LogErr:       filepath.Join(tmp, "err.log"),
		AutoRestart:  "always",
		RestartDelay: "5s",
		MinUptime:    "0s",
	}
	maxR := 100
	params.MaxRestarts = &maxR

	p, err := d.startProcess(params)
	if err != nil {
		t.Fatalf("startProcess: %v", err)
	}
	firstInstance := p.Instance()

	// Wait until the process has exited and the supervisor is in its
	// restart-delay sleep (Status == Stopped).
	ok := waitUntil(3*time.Second, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.info.Status == protocol.StatusStopped && p.info.PID == 0
	})
	if !ok {
		t.Fatal("supervisor did not enter restart-delay state within 3s")
	}

	// Cancel via Stop() — should close cancelRestart and abort the restart.
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Supervisor goroutine must return without calling Start().
	time.Sleep(500 * time.Millisecond)

	if got := p.Instance(); got != firstInstance {
		t.Errorf("instance after cancel = %d, want %d (supervisor restarted despite cancel)",
			got, firstInstance)
	}
	p.mu.Lock()
	pid := p.info.PID
	p.mu.Unlock()
	if pid != 0 {
		t.Errorf("PID after cancel = %d, want 0", pid)
		syscall.Kill(pid, syscall.SIGKILL)
	}
}

// TestUserRestartDuringSupervisorDelay reproduces the original production
// bug: a process crash-loops, so the supervisor is in its restart-delay
// sleep. The user then calls handleRestart. The expected behavior is:
//   - Only one live child process at the end.
//   - Instance counter advanced by exactly one (supervisor's pending restart
//     was cancelled; only the user restart took effect).
//   - No orphan children.
func TestUserRestartDuringSupervisorDelay(t *testing.T) {
	d := newTestDaemon(t)

	tmp := t.TempDir()
	// Process that crashes immediately → supervisor enters restart delay.
	params := protocol.StartParams{
		Command:      "/bin/sh",
		Args:         []string{"-c", "exit 1"},
		Name:         "crashloop",
		Cwd:          tmp,
		LogOut:       filepath.Join(tmp, "out.log"),
		LogErr:       filepath.Join(tmp, "err.log"),
		AutoRestart:  "always",
		RestartDelay: "3s",
		MinUptime:    "0s",
	}
	maxR := 100
	params.MaxRestarts = &maxR

	p, err := d.startProcess(params)
	if err != nil {
		t.Fatalf("startProcess: %v", err)
	}

	// Wait for supervisor to enter its restart delay (Status=Stopped, PID=0).
	ok := waitUntil(3*time.Second, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.info.Status == protocol.StatusStopped && p.info.PID == 0
	})
	if !ok {
		t.Fatal("supervisor did not enter restart-delay state")
	}
	instanceBeforeRestart := p.Instance()

	// User calls handleRestart: simulate with a long-running cmd so we can
	// observe the result.
	longParams := params
	longParams.Command = "/bin/sleep"
	longParams.Args = []string{"30"}

	// Mimic handleRestart behavior: Stop (cancels supervisor), then Start.
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Swap command so the new start runs sleep, not the crashing sh.
	p.mu.Lock()
	p.info.Command = longParams.Command
	p.info.Args = longParams.Args
	p.mu.Unlock()
	if err := p.Start("user-restart"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	go d.monitor(p)

	// Give the cancelled supervisor goroutine time to return.
	time.Sleep(500 * time.Millisecond)

	// Verify exactly one Start() took effect (user's). Supervisor's pending
	// restart was cancelled so it must NOT have bumped the instance again.
	instanceAfterRestart := p.Instance()
	if instanceAfterRestart != instanceBeforeRestart+1 {
		t.Errorf("instance after user restart = %d, want %d (supervisor restart leaked through)",
			instanceAfterRestart, instanceBeforeRestart+1)
	}

	p.mu.Lock()
	currentPID := p.info.PID
	currentStatus := p.info.Status
	p.mu.Unlock()

	if currentStatus != protocol.StatusOnline {
		t.Errorf("status after restart = %q, want online", currentStatus)
	}
	if !processAlive(currentPID) {
		t.Errorf("current PID %d not alive after user restart", currentPID)
	}

	p.Stop()
}

// TestLifecycleCountersBumpCorrectly verifies each Start/Stop/crash path
// advances the right counters, which feed telegraf telemetry.
func TestLifecycleCountersBumpCorrectly(t *testing.T) {
	d := newTestDaemon(t)
	p := startLongRunning(t, d, "lifecycle")

	info := p.Info()
	if info.StartCount != 1 {
		t.Errorf("StartCount after initial start = %d, want 1", info.StartCount)
	}
	if info.Instance != 1 {
		t.Errorf("Instance after initial start = %d, want 1", info.Instance)
	}

	// User restart: Stop + Start("user-restart") + monitor.
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := p.Start("user-restart"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	go d.monitor(p)

	info = p.Info()
	if info.StartCount != 2 {
		t.Errorf("StartCount after user-restart = %d, want 2", info.StartCount)
	}
	if info.StopCount != 1 {
		t.Errorf("StopCount = %d, want 1", info.StopCount)
	}
	if info.UserRestartCount != 1 {
		t.Errorf("UserRestartCount = %d, want 1", info.UserRestartCount)
	}
	if info.SupervisorRestartCount != 0 {
		t.Errorf("SupervisorRestartCount = %d, want 0", info.SupervisorRestartCount)
	}

	p.Stop()
}

// TestCrashCountBumpsOnNonZeroExit uses a short-lived crashing process with
// autorestart=never so the supervisor doesn't loop in the background.
func TestCrashCountBumpsOnNonZeroExit(t *testing.T) {
	d := newTestDaemon(t)

	tmp := t.TempDir()
	params := protocol.StartParams{
		Command:     "/bin/sh",
		Args:        []string{"-c", "exit 42"},
		Name:        "crasher",
		Cwd:         tmp,
		LogOut:      filepath.Join(tmp, "out.log"),
		LogErr:      filepath.Join(tmp, "err.log"),
		AutoRestart: "never",
	}
	maxR := 0
	params.MaxRestarts = &maxR

	p, err := d.startProcess(params)
	if err != nil {
		t.Fatalf("startProcess: %v", err)
	}

	// Wait for crash + exit handling to complete.
	ok := waitUntil(3*time.Second, func() bool {
		info := p.Info()
		return info.CrashCount >= 1
	})
	if !ok {
		info := p.Info()
		t.Fatalf("CrashCount did not increase to >= 1 (got %d)", info.CrashCount)
	}
	info := p.Info()
	if info.LastExitCode != 42 {
		t.Errorf("LastExitCode = %d, want 42", info.LastExitCode)
	}
	if info.LastRunDuration <= 0 {
		t.Errorf("LastRunDuration = %d, want > 0", info.LastRunDuration)
	}
}

// TestConcurrentRestartNoOrphans exercises the production race: rapid
// user-initiated restarts on a process that also has supervisor restarts
// in flight. No orphan children may be left behind.
func TestConcurrentRestartNoOrphans(t *testing.T) {
	d := newTestDaemon(t)

	tmp := t.TempDir()
	params := protocol.StartParams{
		Command:      "/bin/sleep",
		Args:         []string{"30"},
		Name:         "racing",
		Cwd:          tmp,
		LogOut:       filepath.Join(tmp, "out.log"),
		LogErr:       filepath.Join(tmp, "err.log"),
		AutoRestart:  "always",
		RestartDelay: "100ms",
		MinUptime:    "0s",
	}
	maxR := 1000
	params.MaxRestarts = &maxR

	p, err := d.startProcess(params)
	if err != nil {
		t.Fatalf("startProcess: %v", err)
	}

	// Fire many sequential user-restarts rapidly.
	var pids []int
	for i := 0; i < 10; i++ {
		p.Stop()
		if err := p.Start("user-restart"); err != nil {
			t.Fatalf("Start %d: %v", i, err)
		}
		go d.monitor(p)
		p.mu.Lock()
		pids = append(pids, p.info.PID)
		p.mu.Unlock()
		time.Sleep(30 * time.Millisecond)
	}

	// Allow monitor/supervisor goroutines to settle.
	time.Sleep(1 * time.Second)

	p.mu.Lock()
	currentPID := p.info.PID
	p.mu.Unlock()

	if !processAlive(currentPID) {
		t.Errorf("current PID %d is not alive", currentPID)
	}

	// Every earlier PID must be dead.
	for _, pid := range pids {
		if pid == currentPID {
			continue
		}
		if processAlive(pid) {
			t.Errorf("orphan process still alive: pid=%d (current=%d)", pid, currentPID)
			syscall.Kill(pid, syscall.SIGKILL)
		}
	}

	p.Stop()
}

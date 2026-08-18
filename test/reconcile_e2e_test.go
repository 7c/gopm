package test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// TestResurrectReconcilesOrphanBeforeSpawn is the end-to-end regression test
// for the "duplicate process after daemon restart" bug the reconcile pass
// fixes. Setup:
//
//  1. Spawn testapp manually (outside gopm) with GOPM_MANAGED_NAME=<name>
//     and Setpgid=true — simulating a child that survived a previous
//     daemon session's uncleanly-exited process (SIGKILL/OOM/host crash).
//  2. Write dump.json with an entry for <name>, status=online, pointing
//     at an unrelated PID (the daemon uses the env marker on Linux and
//     argv on Darwin — it never trusts the saved PID as an alive-signal).
//  3. Any CLI call auto-starts the daemon, which auto-resurrects. Before
//     spawning, reconcile must find and kill our orphan.
//
// Assertion: the orphan PID is dead AND a fresh process with the same
// name is online under a different PID.
func TestResurrectReconcilesOrphanBeforeSpawn(t *testing.T) {
	env := NewTestEnv(t)

	name := fmt.Sprintf("orphan-e2e-%d", os.Getpid())

	// 1) Spawn an orphan tagged with the marker. Give it a distinctive
	//    argv so Darwin's argv-based matcher also finds it.
	orphanCmd := exec.Command(env.TestappBin,
		"--run-forever",
		"--stdout-msg", "orphan-marker-"+name,
	)
	orphanCmd.Env = append(os.Environ(),
		"GOPM_MANAGED_NAME="+name,
		"GOPM_MANAGED_ID=999",
	)
	orphanCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := orphanCmd.Start(); err != nil {
		t.Fatalf("spawn orphan: %v", err)
	}
	orphanPID := orphanCmd.Process.Pid
	t.Logf("orphan spawned: PID=%d", orphanPID)
	t.Cleanup(func() {
		// Best-effort teardown in case reconcile didn't get to it.
		pgid, _ := syscall.Getpgid(orphanPID)
		if pgid > 0 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
		_, _ = orphanCmd.Process.Wait()
	})

	// Give it a beat to be visible to /proc or ps.
	time.Sleep(200 * time.Millisecond)

	// 2) Write dump.json with a fake PID. reconcile finds orphans by
	//    env marker (linux) / argv (darwin), NOT by the saved PID, so
	//    the PID we write here is irrelevant — using a value we KNOW
	//    is not the orphan proves reconcile isn't relying on it.
	dump := []map[string]interface{}{
		{
			"id":      0,
			"name":    name,
			"command": env.TestappBin,
			"args":    []string{"--run-forever", "--stdout-msg", "orphan-marker-" + name},
			"cwd":     env.Home,
			"env":     map[string]string{},
			"status":  "online",
			"pid":     42, // deliberately unrelated
			"restart_policy": map[string]interface{}{
				"autorestart":   "always",
				"max_restarts":  0,
				"min_uptime":    "5s",
				"restart_delay": "500ms",
				"exp_backoff":   false,
				"max_delay":     "30s",
				"kill_signal":   15,
				"kill_timeout":  "2s",
			},
			"log_out":      filepath.Join(env.Home, "logs", name+"-out.log"),
			"log_err":      filepath.Join(env.Home, "logs", name+"-err.log"),
			"max_log_size": 104857600,
		},
	}
	data, err := json.Marshal(dump)
	if err != nil {
		t.Fatalf("marshal dump: %v", err)
	}
	if err := os.WriteFile(filepath.Join(env.Home, "dump.json"), data, 0644); err != nil {
		t.Fatalf("write dump: %v", err)
	}

	// 3) Any CLI call auto-starts the daemon, which resurrects → reconcile.
	env.MustGopm("ping")

	// The process should end up online with a fresh PID.
	env.WaitForStatus(name, "online", 10*time.Second)

	newPIDStr := env.GetProcessField(name, "pid")
	newPID, err := strconv.Atoi(newPIDStr)
	if err != nil || newPID <= 0 {
		t.Fatalf("expected non-zero fresh PID after resurrect+reconcile, got %q", newPIDStr)
	}
	if newPID == orphanPID {
		t.Fatalf("daemon reported the orphan PID %d instead of spawning a fresh one", orphanPID)
	}

	// Give reconcile+reap a moment to finalize.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pidEffectivelyGone(t, orphanPID) {
			t.Logf("orphan PID %d dead; fresh PID %d online — reconcile worked", orphanPID, newPID)
			assertDaemonLogHasReconcile(t, env, name)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("orphan PID %d survived reconcile", orphanPID)
}

// assertDaemonLogHasReconcile confirms the daemon actually emitted the
// reconcile trace to daemon.log — this is the debug story the operator
// gets when they investigate a startup, so it's part of the contract.
func assertDaemonLogHasReconcile(t *testing.T, env *TestEnv, procName string) {
	t.Helper()
	logPath := filepath.Join(env.Home, "daemon.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read daemon.log: %v", err)
	}
	s := string(data)
	// Look for the key structured-log messages reconcileOrphans emits.
	needles := []string{
		"reconcile: orphan(s) detected",
		"reconcile: sent SIGTERM to orphan pgroup",
		"name=" + procName,
	}
	for _, n := range needles {
		if !containsStr(s, n) {
			t.Errorf("daemon.log missing expected trace %q\n---\n%s\n---", n, s)
		}
	}
}

func containsStr(hay, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// pidEffectivelyGone returns true when pid is either fully gone (ESRCH) or a
// zombie (dead but not yet reaped). We're not necessarily the orphan's
// parent (we spawned it but the daemon didn't adopt it), so a zombie can
// linger a beat until init reaps.
func pidEffectivelyGone(t *testing.T, pid int) bool {
	t.Helper()
	// Try to reap if we're the parent.
	var status syscall.WaitStatus
	_, _ = syscall.Wait4(pid, &status, syscall.WNOHANG, nil)

	if err := syscall.Kill(pid, 0); err != nil {
		return err == syscall.ESRCH
	}
	// Alive per kill(0). Check for zombie state on both platforms.
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	if err != nil {
		return true
	}
	s := string(out)
	if len(s) == 0 {
		return true
	}
	// Both Linux and Darwin `ps -o stat=` use 'Z' for zombies.
	for i := 0; i < len(s); i++ {
		if s[i] == 'Z' {
			return true
		}
		if s[i] == ' ' || s[i] == '\n' || s[i] == '\t' {
			break
		}
	}
	return false
}

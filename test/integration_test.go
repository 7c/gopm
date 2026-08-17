package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPingStartsDaemon(t *testing.T) {
	env := NewTestEnv(t)
	out := env.MustGopm("ping")
	if !strings.Contains(out, "daemon") && !strings.Contains(out, "running") {
		t.Errorf("ping output unexpected: %q", out)
	}
}

func TestStartAndList(t *testing.T) {
	env := NewTestEnv(t)

	out := env.MustGopm("start", env.TestappBin, "--name", "proc1", "--", "--run-forever")
	if !strings.Contains(out, "proc1") || !strings.Contains(out, "started") {
		t.Errorf("start output unexpected: %q", out)
	}

	time.Sleep(500 * time.Millisecond)

	out = env.MustGopm("list", "--json")
	var procs []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &procs); err != nil {
		t.Fatalf("parse list output: %v", err)
	}
	if len(procs) != 1 {
		t.Fatalf("expected 1 process, got %d", len(procs))
	}
	if procs[0]["name"] != "proc1" {
		t.Errorf("process name = %q, want proc1", procs[0]["name"])
	}
	if procs[0]["status"] != "online" {
		t.Errorf("process status = %q, want online", procs[0]["status"])
	}
}

func TestStopProcess(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "stopme", "--", "--run-forever")
	time.Sleep(500 * time.Millisecond)
	env.WaitForStatus("stopme", "online", 5*time.Second)

	env.MustGopm("stop", "stopme")
	env.WaitForStatus("stopme", "stopped", 5*time.Second)

	status := env.GetProcessField("stopme", "status")
	if status != "stopped" {
		t.Errorf("status = %q, want stopped", status)
	}
}

func TestRestartProcess(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "restartme", "--", "--run-forever")
	env.WaitForStatus("restartme", "online", 5*time.Second)

	pidBefore := env.GetProcessField("restartme", "pid")
	env.MustGopm("restart", "restartme")
	time.Sleep(500 * time.Millisecond)
	env.WaitForStatus("restartme", "online", 5*time.Second)
	pidAfter := env.GetProcessField("restartme", "pid")

	if pidBefore == pidAfter {
		t.Errorf("PID should change after restart, got %s both times", pidBefore)
	}
}

// TestRestartFailureMarksErrored verifies that when a `gopm restart` cannot
// start the new instance (missing binary, bad interpreter, etc.), the process
// ends up as `errored` with a status_reason that reflects the real cause —
// not left as the transient `stopped (restarting)` that used to persist
// forever because the auto-restart supervisor doesn't cycle on failed
// user-initiated restarts.
func TestRestartFailureMarksErrored(t *testing.T) {
	env := NewTestEnv(t)

	// Copy the testapp binary so we can delete it out from under gopm
	// without disturbing other tests.
	bin := filepath.Join(env.Home, "temp-testapp")
	src, err := os.ReadFile(env.TestappBin)
	if err != nil {
		t.Fatalf("read testapp: %v", err)
	}
	if err := os.WriteFile(bin, src, 0755); err != nil {
		t.Fatalf("write temp binary: %v", err)
	}

	env.MustGopm("start", bin, "--name", "flaky", "--", "--run-forever")
	env.WaitForStatus("flaky", "online", 5*time.Second)

	// Remove the binary so the Start after Stop in handleRestart fails.
	if err := os.Remove(bin); err != nil {
		t.Fatalf("remove temp binary: %v", err)
	}

	// gopm restart will not error at the RPC level — the daemon still
	// returns success with the (now errored) ProcessInfo. We just care
	// about the resulting persisted state.
	env.Gopm("restart", "flaky")

	env.WaitForStatus("flaky", "errored", 5*time.Second)

	reason := env.GetProcessField("flaky", "status_reason")
	if !strings.Contains(reason, "restart failed") {
		t.Errorf("status_reason = %q, want it to contain %q", reason, "restart failed")
	}
	if reason == "restarting" {
		t.Errorf("status_reason left as the transient %q — display would show 'stopped (restarting)' forever", reason)
	}
}

func TestDeleteProcess(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "deleteme", "--", "--run-forever")
	env.WaitForStatus("deleteme", "online", 5*time.Second)

	env.MustGopm("delete", "deleteme")
	time.Sleep(300 * time.Millisecond)

	count := env.ProcessCount()
	if count != 0 {
		t.Errorf("expected 0 processes after delete, got %d", count)
	}
}

func TestDescribe(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "info", "--", "--run-forever")
	env.WaitForStatus("info", "online", 5*time.Second)

	out := env.MustGopm("describe", "info", "--json")
	var proc map[string]interface{}
	if err := json.Unmarshal([]byte(out), &proc); err != nil {
		t.Fatalf("parse describe output: %v", err)
	}
	if proc["name"] != "info" {
		t.Errorf("name = %q", proc["name"])
	}
	if proc["status"] != "online" {
		t.Errorf("status = %q", proc["status"])
	}
	if proc["command"] != env.TestappBin {
		t.Errorf("command = %q", proc["command"])
	}
}

func TestIsRunning(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "chk", "--", "--run-forever")
	env.WaitForStatus("chk", "online", 5*time.Second)

	_, _, code := env.Gopm("isrunning", "chk")
	if code != 0 {
		t.Errorf("isrunning should exit 0 for online process, got %d", code)
	}

	env.MustGopm("stop", "chk")
	env.WaitForStatus("chk", "stopped", 5*time.Second)

	_, _, code = env.Gopm("isrunning", "chk")
	if code != 1 {
		t.Errorf("isrunning should exit 1 for stopped process, got %d", code)
	}

	// Non-existent
	_, _, code = env.Gopm("isrunning", "nonexistent")
	if code != 1 {
		t.Errorf("isrunning should exit 1 for non-existent process, got %d", code)
	}
}

// TestIsOnlineAlias verifies isonline is a functional alias for isrunning —
// same exit codes, same JSON payload, same daemon call.
func TestIsOnlineAlias(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "chk", "--", "--run-forever")
	env.WaitForStatus("chk", "online", 5*time.Second)

	_, _, code := env.Gopm("isonline", "chk")
	if code != 0 {
		t.Errorf("isonline should exit 0 for online process, got %d", code)
	}

	env.MustGopm("stop", "chk")
	env.WaitForStatus("chk", "stopped", 5*time.Second)

	_, _, code = env.Gopm("isonline", "chk")
	if code != 1 {
		t.Errorf("isonline should exit 1 for stopped process, got %d", code)
	}

	// The alias should return the same --json payload as isrunning does.
	env.MustGopm("start", env.TestappBin, "--name", "chk2", "--", "--run-forever")
	env.WaitForStatus("chk2", "online", 5*time.Second)
	runningJSON, _, _ := env.Gopm("isrunning", "chk2", "--json")
	onlineJSON, _, _ := env.Gopm("isonline", "chk2", "--json")
	if runningJSON != onlineJSON {
		t.Errorf("isonline --json diverged from isrunning --json:\n  isrunning: %s  isonline:  %s", runningJSON, onlineJSON)
	}
}

// TestIsStopped covers the contract: exit 0 once a Stop has ever been
// requested (stop_count > 0), exit 1 for a fresh never-stopped process or one
// gopm does not know about. Currently-online-but-once-stopped still returns 0
// — the check is historical, not "stopped right now".
func TestIsStopped(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "stopchk", "--", "--run-forever")
	env.WaitForStatus("stopchk", "online", 5*time.Second)

	// Fresh process — never stopped.
	_, _, code := env.Gopm("isstopped", "stopchk")
	if code != 1 {
		t.Errorf("isstopped should exit 1 for a never-stopped process, got %d", code)
	}

	env.MustGopm("stop", "stopchk")
	env.WaitForStatus("stopchk", "stopped", 5*time.Second)

	_, _, code = env.Gopm("isstopped", "stopchk")
	if code != 0 {
		t.Errorf("isstopped should exit 0 after a user stop, got %d", code)
	}

	// Historical: restart it, and isstopped must still say yes.
	env.MustGopm("restart", "stopchk")
	env.WaitForStatus("stopchk", "online", 5*time.Second)
	_, _, code = env.Gopm("isstopped", "stopchk")
	if code != 0 {
		t.Errorf("isstopped must remain 0 for a once-stopped-now-online process (stop_count > 0), got %d", code)
	}

	// Unknown process.
	_, _, code = env.Gopm("isstopped", "nonexistent")
	if code != 1 {
		t.Errorf("isstopped should exit 1 for unknown process, got %d", code)
	}

	// --json exposes the stop_count.
	out, _, _ := env.Gopm("isstopped", "stopchk", "--json")
	var payload struct {
		StopCount int `json:"stop_count"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse isstopped --json: %v\noutput: %s", err, out)
	}
	if payload.StopCount < 1 {
		t.Errorf("stop_count = %d in --json, expected >= 1 after a user stop", payload.StopCount)
	}
}

// TestIsProcess covers the contract isprocess adds over isrunning: existence in
// any state, and a distinct exit code when the daemon cannot be reached.
func TestIsProcess(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "chk", "--", "--run-forever")
	env.WaitForStatus("chk", "online", 5*time.Second)

	_, _, code := env.Gopm("isprocess", "chk")
	if code != 0 {
		t.Errorf("isprocess should exit 0 for online process, got %d", code)
	}

	// The distinguishing case: stopped still exists, so isprocess exits 0
	// where isrunning exits 1.
	env.MustGopm("stop", "chk")
	env.WaitForStatus("chk", "stopped", 5*time.Second)

	_, _, code = env.Gopm("isprocess", "chk")
	if code != 0 {
		t.Errorf("isprocess should exit 0 for stopped process, got %d", code)
	}
	if _, _, rcode := env.Gopm("isrunning", "chk"); rcode != 1 {
		t.Errorf("isrunning should still exit 1 for stopped process, got %d", rcode)
	}

	// Errored also counts as existing.
	env.MustGopm("start", env.TestappBin, "--name", "crasher",
		"--autorestart", "on-failure", "--max-restarts", "1", "--restart-delay", "500ms",
		"--", "--crash-after", "500ms", "--exit-code", "1")
	env.WaitForStatus("crasher", "errored", 20*time.Second)

	_, _, code = env.Gopm("isprocess", "crasher")
	if code != 0 {
		t.Errorf("isprocess should exit 0 for errored process, got %d", code)
	}

	// Known daemon, unknown process.
	_, _, code = env.Gopm("isprocess", "nonexistent")
	if code != 1 {
		t.Errorf("isprocess should exit 1 for non-existent process, got %d", code)
	}

	// Unreachable daemon is its own exit code, and must not be papered over by
	// auto-starting one.
	env.MustGopm("kill")
	env.WaitForDaemonStopped(5 * time.Second)

	_, _, code = env.Gopm("isprocess", "chk")
	if code != 2 {
		t.Errorf("isprocess should exit 2 when the daemon is down, got %d", code)
	}
	// If the call above had auto-started a daemon, this one would reach it and
	// report "not found" (exit 1) against an empty process table.
	_, _, code = env.Gopm("isprocess", "chk")
	if code != 2 {
		t.Errorf("isprocess must not auto-start a daemon; second call exited %d, want 2", code)
	}
}

func TestIsProcessJSON(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "jchk", "--", "--run-forever")
	env.WaitForStatus("jchk", "online", 5*time.Second)

	out, _, code := env.Gopm("isprocess", "jchk", "--json")
	if code != 0 {
		t.Fatalf("isprocess --json exited %d: %s", code, out)
	}
	var result struct {
		Name   string `json:"name"`
		Exists bool   `json:"exists"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parsing isprocess --json output %q: %v", out, err)
	}
	if !result.Exists || result.Name != "jchk" || result.Status != "online" {
		t.Errorf("got %+v, want name=jchk exists=true status=online", result)
	}

	out, _, code = env.Gopm("isprocess", "nonexistent", "--json")
	if code != 1 {
		t.Fatalf("isprocess --json exited %d for missing process, want 1", code)
	}
	result.Exists = true
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parsing isprocess --json output %q: %v", out, err)
	}
	if result.Exists {
		t.Errorf("exists should be false for a missing process, got %+v", result)
	}
}

func TestLogs(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "logger", "--",
		"--run-forever", "--stdout-every", "500ms")
	time.Sleep(2 * time.Second)

	out := env.MustGopm("logs", "logger", "--lines", "3")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Errorf("expected at least 2 log lines, got %d: %q", len(lines), out)
	}
	// Lines should have timestamps
	for _, line := range lines {
		if len(line) > 0 && !strings.Contains(line, "T") {
			t.Errorf("log line missing timestamp: %q", line)
		}
	}
}

func TestFlushLogs(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "flusher", "--",
		"--run-forever", "--stdout-every", "200ms")
	time.Sleep(1 * time.Second)

	env.MustGopm("flush", "flusher")
	time.Sleep(500 * time.Millisecond)

	out := env.MustGopm("logs", "flusher", "--lines", "100", "--json")
	var result map[string]interface{}
	json.Unmarshal([]byte(out), &result)
	content, _ := result["content"].(string)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	// After flush + 500ms at 200ms intervals, should have few lines
	if len(lines) > 5 {
		t.Errorf("expected few lines after flush, got %d", len(lines))
	}
}

func TestSaveAndResurrect(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "saver1", "--", "--run-forever")
	env.MustGopm("start", env.TestappBin, "--name", "saver2", "--", "--run-forever")
	env.WaitForStatus("saver1", "online", 5*time.Second)
	env.WaitForStatus("saver2", "online", 5*time.Second)

	// State is auto-saved after start — no explicit save needed.
	// Give daemon a moment to persist.
	time.Sleep(300 * time.Millisecond)

	// Kill daemon (processes die with it, but dump.json remains)
	env.Gopm("kill")
	time.Sleep(1 * time.Second)

	// Start a fresh daemon and resurrect from dump.json
	out := env.MustGopm("resurrect")
	if !strings.Contains(strings.ToLower(out), "resurrected") {
		t.Errorf("resurrect output unexpected: %q", out)
	}

	env.WaitForStatus("saver1", "online", 5*time.Second)
	env.WaitForStatus("saver2", "online", 5*time.Second)

	count := env.ProcessCount()
	if count != 2 {
		t.Errorf("expected 2 processes after resurrect, got %d", count)
	}
}

// TestResurrectResumesRestartDelay is a regression test for the case where
// `gopm reboot` (or any daemon restart) fires while a supervised process is
// inside its restart-delay window. That transient state persists as
// status=stopped + in_restart_delay=true. Prior to the fix, resurrect only
// started processes with status=online, so the process was silently orphaned
// as "stopped" until a manual `gopm start` — despite autorestart=always and
// no user-issued stop.
//
// We reproduce the on-disk state directly rather than racing against real
// timing, then start a fresh daemon (any CLI call auto-starts it), which
// auto-resurrects from dump.json.
func TestResurrectResumesRestartDelay(t *testing.T) {
	env := NewTestEnv(t)

	// Craft the exact dump.json state that used to orphan the process:
	// mid-restart-delay when the daemon went down.
	dump := []map[string]interface{}{
		{
			"id":               0,
			"name":             "delayed",
			"command":          env.TestappBin,
			"args":             []string{"--run-forever"},
			"cwd":              env.Home,
			"env":              map[string]string{},
			"status":           "stopped",
			"in_restart_delay": true,
			"pid":              0,
			"restart_policy": map[string]interface{}{
				"autorestart":   "always",
				"max_restarts":  0,
				"min_uptime":    "5s",
				"restart_delay": "500ms",
				"exp_backoff":   false,
				"max_delay":     "30s",
				"kill_signal":   15,
				"kill_timeout":  "5s",
			},
			"log_out":      filepath.Join(env.Home, "logs", "delayed-out.log"),
			"log_err":      filepath.Join(env.Home, "logs", "delayed-err.log"),
			"max_log_size": 104857600,
		},
	}
	data, err := json.Marshal(dump)
	if err != nil {
		t.Fatalf("marshal dump: %v", err)
	}
	if err := os.WriteFile(filepath.Join(env.Home, "dump.json"), data, 0644); err != nil {
		t.Fatalf("write dump.json: %v", err)
	}

	// Any CLI call auto-starts the daemon, which auto-resurrects on boot.
	env.MustGopm("ping")

	// Before the fix, the process stayed "stopped" indefinitely.
	env.WaitForStatus("delayed", "online", 10*time.Second)

	if pid := env.GetProcessField("delayed", "pid"); pid == "" || pid == "0" {
		t.Errorf("expected non-zero pid after resurrect, got %q", pid)
	}
}

// TestStopDuringRestartDelaySurvivesDaemonRestart is the counterpart to
// TestResurrectResumesRestartDelay: when the user stops a crash-looping
// process and the daemon then goes down (kill / crash / reboot), the process
// must NOT come back on resurrect. The resume-on-in-restart-delay fix would
// otherwise revive it, so Stop() has to eagerly clear InRestartDelay before
// SaveState observes it.
func TestStopDuringRestartDelaySurvivesDaemonRestart(t *testing.T) {
	env := NewTestEnv(t)

	// exit-after 100ms + restart-delay 3s means the process spends almost
	// its entire life inside the restart-delay window — trivially catches
	// the "user stops while in delay" case.
	env.MustGopm("start", env.TestappBin, "--name", "stopdelay",
		"--autorestart", "always",
		"--restart-delay", "3s",
		"--", "--exit-after", "100ms")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if env.GetProcessField("stopdelay", "in_restart_delay") == "true" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if env.GetProcessField("stopdelay", "in_restart_delay") != "true" {
		t.Fatal("process never entered restart-delay window")
	}

	env.MustGopm("stop", "stopdelay")
	env.WaitForStatus("stopdelay", "stopped", 3*time.Second)

	env.MustGopm("kill")
	env.WaitForDaemonStopped(5 * time.Second)
	env.MustGopm("ping")

	// A brief settle so a would-be resurrect-started process has time to
	// show up as online. The stop must stick.
	time.Sleep(1 * time.Second)
	if status := env.GetProcessField("stopdelay", "status"); status != "stopped" {
		t.Errorf("status = %q after daemon restart, want stopped — user-stop was undone by resurrect", status)
	}
	if env.GetProcessField("stopdelay", "in_restart_delay") == "true" {
		t.Error("in_restart_delay = true after user stop + daemon restart")
	}
}

func TestAutoRestartOnFailure(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "crasher",
		"--autorestart", "on-failure",
		"--max-restarts", "2",
		"--restart-delay", "500ms",
		"--", "--crash-after", "1s", "--exit-code", "1")

	// Wait for max restarts to be reached:
	// Initial run ~1s + restart1 (500ms delay + ~1s run) + restart2 (500ms delay + ~1s run)
	// then marks errored = ~5s total. Give 20s for safety.
	env.WaitForRestartCount("crasher", 2, 20*time.Second)

	// After max restarts, wait a bit for the errored state to be set
	time.Sleep(2 * time.Second)

	status := env.GetProcessField("crasher", "status")
	if status != "errored" {
		t.Errorf("status = %q, want errored", status)
	}
}

func TestAutoRestartNever(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "oneshot",
		"--autorestart", "never",
		"--", "--exit-after", "1s")

	time.Sleep(3 * time.Second)

	status := env.GetProcessField("oneshot", "status")
	if status != "stopped" {
		t.Errorf("status = %q, want stopped", status)
	}
	restarts := env.GetProcessField("oneshot", "restarts")
	if restarts != "0" {
		t.Errorf("restarts = %q, want 0", restarts)
	}
}

func TestKillDaemon(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("ping") // ensure daemon is running

	out, _, code := env.Gopm("kill")
	if code != 0 {
		t.Errorf("kill exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "stopped") {
		t.Errorf("kill output unexpected: %q", out)
	}

	time.Sleep(500 * time.Millisecond)

	// Daemon should be gone - next ping will auto-start a new one
	// We just verify the kill didn't error
}

func TestJSONOutput(t *testing.T) {
	env := NewTestEnv(t)

	out := env.MustGopm("ping", "--json")
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("ping --json not valid JSON: %v\noutput: %q", err, out)
	}
	if _, ok := result["pid"]; !ok {
		t.Error("ping --json missing 'pid' field")
	}
}

func TestDuplicateNameError(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "dup", "--", "--run-forever")
	env.WaitForStatus("dup", "online", 5*time.Second)

	_, stderr, code := env.Gopm("start", env.TestappBin, "--name", "dup", "--", "--run-forever")
	if code == 0 {
		t.Error("starting duplicate name should fail")
	}
	if !strings.Contains(stderr, "already exists") && !strings.Contains(stderr, "dup") {
		t.Errorf("error should mention duplicate: %q", stderr)
	}
}

func TestStopAll(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "all1", "--", "--run-forever")
	env.MustGopm("start", env.TestappBin, "--name", "all2", "--", "--run-forever")
	env.WaitForStatus("all1", "online", 5*time.Second)
	env.WaitForStatus("all2", "online", 5*time.Second)

	env.MustGopm("stop", "all")
	time.Sleep(1 * time.Second)

	s1 := env.GetProcessField("all1", "status")
	s2 := env.GetProcessField("all2", "status")
	if s1 != "stopped" || s2 != "stopped" {
		t.Errorf("statuses = %q, %q; want both stopped", s1, s2)
	}
}

func TestStopMultipleTargets(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "m1", "--", "--run-forever")
	env.MustGopm("start", env.TestappBin, "--name", "m2", "--", "--run-forever")
	env.MustGopm("start", env.TestappBin, "--name", "m3", "--", "--run-forever")
	env.WaitForStatus("m1", "online", 5*time.Second)
	env.WaitForStatus("m2", "online", 5*time.Second)
	env.WaitForStatus("m3", "online", 5*time.Second)

	// Stop two, leave m3 running.
	env.MustGopm("stop", "m1", "m2")
	env.WaitForStatus("m1", "stopped", 5*time.Second)
	env.WaitForStatus("m2", "stopped", 5*time.Second)

	if s := env.GetProcessField("m3", "status"); s != "online" {
		t.Errorf("m3 status = %q, want still online", s)
	}
}

func TestStopMultipleTargets_PartialFailureExitsNonZero(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "ok1", "--", "--run-forever")
	env.WaitForStatus("ok1", "online", 5*time.Second)

	// One valid target, one bogus — expect exit != 0 but ok1 still stopped.
	_, stderr, code := env.Gopm("stop", "ok1", "does-not-exist")
	if code == 0 {
		t.Error("expected non-zero exit when any target fails")
	}
	if !strings.Contains(stderr, "does-not-exist") {
		t.Errorf("stderr should mention failing target: %q", stderr)
	}
	env.WaitForStatus("ok1", "stopped", 5*time.Second)
}

func TestStopMultipleTargets_JSONOutput(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "j1", "--", "--run-forever")
	env.MustGopm("start", env.TestappBin, "--name", "j2", "--", "--run-forever")
	env.WaitForStatus("j1", "online", 5*time.Second)
	env.WaitForStatus("j2", "online", 5*time.Second)

	out := env.MustGopm("--json", "stop", "j1", "j2")
	var results []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("expected JSON array, got %q: %v", out, err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(results), results)
	}
	for _, r := range results {
		if r["success"] != true {
			t.Errorf("expected success for %v", r)
		}
	}
}

func TestRestartMultipleTargets(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "r1", "--", "--run-forever")
	env.MustGopm("start", env.TestappBin, "--name", "r2", "--", "--run-forever")
	env.WaitForStatus("r1", "online", 5*time.Second)
	env.WaitForStatus("r2", "online", 5*time.Second)

	pid1Before := env.GetProcessField("r1", "pid")
	pid2Before := env.GetProcessField("r2", "pid")

	env.MustGopm("restart", "r1", "r2")
	env.WaitForStatus("r1", "online", 5*time.Second)
	env.WaitForStatus("r2", "online", 5*time.Second)

	pid1After := env.GetProcessField("r1", "pid")
	pid2After := env.GetProcessField("r2", "pid")

	if pid1Before == pid1After {
		t.Errorf("r1 PID should have changed, got %s both times", pid1After)
	}
	if pid2Before == pid2After {
		t.Errorf("r2 PID should have changed, got %s both times", pid2After)
	}
}

func TestRestartMultipleTargets_JSONArray(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "rj1", "--", "--run-forever")
	env.MustGopm("start", env.TestappBin, "--name", "rj2", "--", "--run-forever")
	env.WaitForStatus("rj1", "online", 5*time.Second)
	env.WaitForStatus("rj2", "online", 5*time.Second)

	out := env.MustGopm("--json", "restart", "rj1", "rj2")
	var procs []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &procs); err != nil {
		t.Fatalf("expected JSON array, got %q: %v", out, err)
	}
	if len(procs) != 2 {
		t.Fatalf("expected 2 process entries, got %d: %v", len(procs), procs)
	}
	names := map[string]bool{}
	for _, p := range procs {
		if n, ok := p["name"].(string); ok {
			names[n] = true
		}
	}
	if !names["rj1"] || !names["rj2"] {
		t.Errorf("expected rj1 and rj2 in restart output, got %v", names)
	}
}

func TestRestartMultipleTargets_PartialFailureExitsNonZero(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "rp1", "--", "--run-forever")
	env.WaitForStatus("rp1", "online", 5*time.Second)
	pidBefore := env.GetProcessField("rp1", "pid")

	_, stderr, code := env.Gopm("restart", "rp1", "does-not-exist")
	if code == 0 {
		t.Error("expected non-zero exit when any restart target fails")
	}
	if !strings.Contains(stderr, "does-not-exist") {
		t.Errorf("stderr should mention failing target: %q", stderr)
	}
	env.WaitForStatus("rp1", "online", 5*time.Second)
	pidAfter := env.GetProcessField("rp1", "pid")
	if pidBefore == pidAfter {
		t.Errorf("rp1 should have been restarted even though the other target failed")
	}
}

func TestAutoLoadDumpOnDaemonStart(t *testing.T) {
	env := NewTestEnv(t)

	// Start two processes
	env.MustGopm("start", env.TestappBin, "--name", "persist1", "--", "--run-forever")
	env.MustGopm("start", env.TestappBin, "--name", "persist2", "--", "--run-forever")
	env.WaitForStatus("persist1", "online", 5*time.Second)
	env.WaitForStatus("persist2", "online", 5*time.Second)

	// State is auto-saved after start — verify dump.json exists
	dumpPath := filepath.Join(env.Home, "dump.json")
	if _, err := os.Stat(dumpPath); os.IsNotExist(err) {
		t.Fatal("dump.json not created after save")
	}

	// Kill daemon (processes die too)
	env.Gopm("kill")
	time.Sleep(1 * time.Second)

	// Start a fresh daemon by issuing any command (auto-starts daemon)
	// The daemon should auto-load dump.json and resurrect the processes
	env.MustGopm("ping")
	time.Sleep(2 * time.Second)

	// Verify both processes are back online
	env.WaitForStatus("persist1", "online", 10*time.Second)
	env.WaitForStatus("persist2", "online", 10*time.Second)

	count := env.ProcessCount()
	if count != 2 {
		t.Errorf("expected 2 processes after auto-load, got %d", count)
	}
}

func TestExtKillRestart(t *testing.T) {
	env := NewTestEnv(t)

	// Start a process with autorestart=always
	env.MustGopm("start", env.TestappBin, "--name", "victim",
		"--autorestart", "always",
		"--restart-delay", "500ms",
		"--", "--run-forever")
	env.WaitForStatus("victim", "online", 5*time.Second)

	pidStr := env.GetProcessField("victim", "pid")
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid == 0 {
		t.Fatalf("could not get PID: %q", pidStr)
	}

	// Kill the process externally with SIGKILL (simulating external kill)
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("failed to kill process: %v", err)
	}

	// Wait for the process to be restarted by the daemon
	env.WaitForStatus("victim", "online", 15*time.Second)

	// Verify PID changed (new process)
	newPidStr := env.GetProcessField("victim", "pid")
	if newPidStr == pidStr {
		t.Errorf("PID should change after external kill and restart, still %s", pidStr)
	}

	// Verify restart count increased
	restarts := env.GetProcessField("victim", "restarts")
	if restarts == "0" {
		t.Error("restarts should be > 0 after external kill")
	}
}

func TestExportAll(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "exp1", "--", "--run-forever")
	env.MustGopm("start", env.TestappBin, "--name", "exp2", "--", "--run-forever")
	env.WaitForStatus("exp1", "online", 5*time.Second)
	env.WaitForStatus("exp2", "online", 5*time.Second)

	out := env.MustGopm("export", "all")
	var eco map[string]interface{}
	if err := json.Unmarshal([]byte(out), &eco); err != nil {
		t.Fatalf("export all not valid JSON: %v\noutput: %s", err, out)
	}
	apps, ok := eco["apps"].([]interface{})
	if !ok || len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %v", eco["apps"])
	}

	// Verify app fields
	for _, a := range apps {
		app := a.(map[string]interface{})
		if app["command"] != env.TestappBin {
			t.Errorf("command = %q, want %q", app["command"], env.TestappBin)
		}
		if app["name"] == nil {
			t.Error("app missing name")
		}
	}
}

func TestExportByName(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "pick1", "--", "--run-forever")
	env.MustGopm("start", env.TestappBin, "--name", "pick2", "--", "--run-forever")
	env.WaitForStatus("pick1", "online", 5*time.Second)
	env.WaitForStatus("pick2", "online", 5*time.Second)

	out := env.MustGopm("export", "pick1")
	var eco map[string]interface{}
	if err := json.Unmarshal([]byte(out), &eco); err != nil {
		t.Fatalf("export by name not valid JSON: %v", err)
	}
	apps := eco["apps"].([]interface{})
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}
	app := apps[0].(map[string]interface{})
	if app["name"] != "pick1" {
		t.Errorf("name = %q, want pick1", app["name"])
	}
}

func TestExportByID(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "byid", "--", "--run-forever")
	env.WaitForStatus("byid", "online", 5*time.Second)

	out := env.MustGopm("export", "0")
	var eco map[string]interface{}
	if err := json.Unmarshal([]byte(out), &eco); err != nil {
		t.Fatalf("export by id not valid JSON: %v", err)
	}
	apps := eco["apps"].([]interface{})
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}
	app := apps[0].(map[string]interface{})
	if app["name"] != "byid" {
		t.Errorf("name = %q, want byid", app["name"])
	}
}

func TestExportFull(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "fullexp", "--", "--run-forever")
	env.WaitForStatus("fullexp", "online", 5*time.Second)

	out := env.MustGopm("export", "--full", "fullexp")
	var eco map[string]interface{}
	if err := json.Unmarshal([]byte(out), &eco); err != nil {
		t.Fatalf("export --full not valid JSON: %v", err)
	}
	apps := eco["apps"].([]interface{})
	app := apps[0].(map[string]interface{})

	// --full should include all restart policy fields even if they're defaults
	requiredFields := []string{"autorestart", "max_restarts", "min_uptime", "restart_delay", "kill_timeout", "max_delay"}
	for _, f := range requiredFields {
		if _, ok := app[f]; !ok {
			t.Errorf("--full export missing field %q", f)
		}
	}

	// Should include log paths
	if _, ok := app["log_out"]; !ok {
		t.Error("--full export missing log_out")
	}
	if _, ok := app["log_err"]; !ok {
		t.Error("--full export missing log_err")
	}
	if _, ok := app["max_log_size"]; !ok {
		t.Error("--full export missing max_log_size")
	}
}

func TestExportWithoutFull(t *testing.T) {
	env := NewTestEnv(t)

	// Start with all defaults — export without --full should have minimal fields
	env.MustGopm("start", env.TestappBin, "--name", "minimal", "--", "--run-forever")
	env.WaitForStatus("minimal", "online", 5*time.Second)

	out := env.MustGopm("export", "minimal")
	var eco map[string]interface{}
	if err := json.Unmarshal([]byte(out), &eco); err != nil {
		t.Fatalf("export not valid JSON: %v", err)
	}
	apps := eco["apps"].([]interface{})
	app := apps[0].(map[string]interface{})

	// Default fields should NOT be present without --full
	defaultFields := []string{"autorestart", "max_restarts", "min_uptime", "restart_delay", "kill_timeout", "log_out", "log_err", "max_log_size"}
	for _, f := range defaultFields {
		if _, ok := app[f]; ok {
			t.Errorf("export without --full should not include default field %q", f)
		}
	}
}

func TestExportNew(t *testing.T) {
	env := NewTestEnv(t)

	out := env.MustGopm("export", "--new")
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("export --new not valid JSON: %v\noutput: %s", err, out)
	}
	if _, ok := cfg["logs"]; !ok {
		t.Error("sample config missing 'logs' section")
	}
}

func TestExportNotFound(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin, "--name", "exists", "--", "--run-forever")
	env.WaitForStatus("exists", "online", 5*time.Second)

	_, _, code := env.Gopm("export", "nonexistent")
	if code == 0 {
		t.Error("export of nonexistent process should fail")
	}
}

func TestImport(t *testing.T) {
	env := NewTestEnv(t)

	// Write an ecosystem file
	ecoPath := env.WriteEcosystem(map[string]interface{}{
		"apps": []map[string]interface{}{
			{
				"name":    "imp1",
				"command": env.TestappBin,
				"args":    []string{"--run-forever"},
			},
		},
	})

	out := env.MustGopm("import", ecoPath)
	if !strings.Contains(out, "OK") {
		t.Errorf("import output should contain OK: %q", out)
	}
	if !strings.Contains(out, "Imported 1/1") {
		t.Errorf("import should report 1/1: %q", out)
	}

	env.WaitForStatus("imp1", "online", 5*time.Second)
	if env.ProcessCount() != 1 {
		t.Errorf("expected 1 process after import, got %d", env.ProcessCount())
	}
}

func TestImportSkipsDuplicate(t *testing.T) {
	env := NewTestEnv(t)

	// Start a process first
	env.MustGopm("start", env.TestappBin, "--name", "dup", "--", "--run-forever")
	env.WaitForStatus("dup", "online", 5*time.Second)

	// Export it, then try to import the same config
	exportOut := env.MustGopm("export", "all")
	ecoPath := filepath.Join(env.Home, "export.json")
	if err := os.WriteFile(ecoPath, []byte(exportOut), 0644); err != nil {
		t.Fatal(err)
	}

	out := env.MustGopm("import", ecoPath)
	if !strings.Contains(out, "SKIP") {
		t.Errorf("import should SKIP existing process: %q", out)
	}
	if !strings.Contains(out, "0/1") || !strings.Contains(out, "1 skipped") {
		t.Errorf("import should report 0/1 (1 skipped): %q", out)
	}

	// Still only 1 process
	if env.ProcessCount() != 1 {
		t.Errorf("expected 1 process (no duplicate), got %d", env.ProcessCount())
	}
}

func TestImportMultiple(t *testing.T) {
	env := NewTestEnv(t)

	ecoPath := env.WriteEcosystem(map[string]interface{}{
		"apps": []map[string]interface{}{
			{
				"name":    "multi1",
				"command": env.TestappBin,
				"args":    []string{"--run-forever"},
			},
			{
				"name":    "multi2",
				"command": env.TestappBin,
				"args":    []string{"--run-forever"},
			},
		},
	})

	out := env.MustGopm("import", ecoPath)
	if !strings.Contains(out, "Imported 2/2") {
		t.Errorf("import should report 2/2: %q", out)
	}

	env.WaitForStatus("multi1", "online", 5*time.Second)
	env.WaitForStatus("multi2", "online", 5*time.Second)
	if env.ProcessCount() != 2 {
		t.Errorf("expected 2 processes, got %d", env.ProcessCount())
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	env := NewTestEnv(t)

	// Start processes with non-default settings
	env.MustGopm("start", env.TestappBin, "--name", "trip1",
		"--autorestart", "on-failure",
		"--restart-delay", "3s",
		"--", "--run-forever")
	env.MustGopm("start", env.TestappBin, "--name", "trip2", "--", "--run-forever")
	env.WaitForStatus("trip1", "online", 5*time.Second)
	env.WaitForStatus("trip2", "online", 5*time.Second)

	// Export
	exportOut := env.MustGopm("export", "all")
	ecoPath := filepath.Join(env.Home, "roundtrip.json")
	if err := os.WriteFile(ecoPath, []byte(exportOut), 0644); err != nil {
		t.Fatal(err)
	}

	// Delete all
	env.MustGopm("delete", "all")
	time.Sleep(500 * time.Millisecond)
	if env.ProcessCount() != 0 {
		t.Fatal("expected 0 after delete all")
	}

	// Import back
	out := env.MustGopm("import", ecoPath)
	if !strings.Contains(out, "Imported 2/2") {
		t.Errorf("round-trip import should report 2/2: %q", out)
	}

	env.WaitForStatus("trip1", "online", 5*time.Second)
	env.WaitForStatus("trip2", "online", 5*time.Second)

	// Verify non-default settings survived
	out = env.MustGopm("describe", "trip1", "--json")
	var proc map[string]interface{}
	json.Unmarshal([]byte(out), &proc)
	rp, _ := proc["restart_policy"].(map[string]interface{})
	if ar, _ := rp["autorestart"].(string); ar != "on-failure" {
		t.Errorf("autorestart = %q, want on-failure", ar)
	}
	if rd, _ := rp["restart_delay"].(string); rd != "3s" {
		t.Errorf("restart_delay = %q, want 3s", rd)
	}
}

func TestRebootQuickExit(t *testing.T) {
	env := NewTestEnv(t)

	// Start a process that exits immediately with code 0
	env.MustGopm("start", env.TestappBin, "--name", "quickexit",
		"--max-restarts", "3",
		"--restart-delay", "200ms",
		"--", "--exit-after", "100ms")

	// Wait for process to hit max restarts and become errored
	time.Sleep(5 * time.Second)

	status := env.GetProcessField("quickexit", "status")
	if status != "errored" && status != "stopped" {
		t.Logf("status after restarts: %s", status)
	}

	// The process should still appear in the list even when errored
	count := env.ProcessCount()
	if count != 1 {
		t.Errorf("expected 1 process (even if errored), got %d", count)
	}

	// Now test reboot with a stable process
	env.MustGopm("delete", "quickexit")

	env.MustGopm("start", env.TestappBin, "--name", "stable", "--", "--run-forever")
	env.WaitForStatus("stable", "online", 5*time.Second)

	// Reboot (state is auto-saved)
	env.MustGopm("reboot")
	time.Sleep(2 * time.Second)

	env.WaitForStatus("stable", "online", 15*time.Second)
	if env.ProcessCount() != 1 {
		t.Errorf("expected 1 process after reboot, got %d", env.ProcessCount())
	}
}

func TestReboot(t *testing.T) {
	env := NewTestEnv(t)

	// Start two processes
	env.MustGopm("start", env.TestappBin, "--name", "rb1", "--", "--run-forever")
	env.MustGopm("start", env.TestappBin, "--name", "rb2", "--", "--run-forever")
	env.WaitForStatus("rb1", "online", 5*time.Second)
	env.WaitForStatus("rb2", "online", 5*time.Second)

	// Get old daemon PID
	oldPID := env.GetProcessField("rb1", "pid")

	// Get daemon PID from ping
	out := env.MustGopm("ping", "--json")
	var pingBefore map[string]interface{}
	json.Unmarshal([]byte(out), &pingBefore)
	daemonPIDBefore := pingBefore["pid"]

	// Reboot
	out = env.MustGopm("reboot")
	if !strings.Contains(out, "rebooted") {
		t.Errorf("reboot output unexpected: %q", out)
	}
	if strings.Contains(out, "PID: 0") {
		t.Errorf("reboot should show actual PID, got: %q", out)
	}

	// Wait for processes to come back online
	env.WaitForStatus("rb1", "online", 15*time.Second)
	env.WaitForStatus("rb2", "online", 15*time.Second)

	// Daemon PID should have changed
	out = env.MustGopm("ping", "--json")
	var pingAfter map[string]interface{}
	json.Unmarshal([]byte(out), &pingAfter)
	if pingAfter["pid"] == daemonPIDBefore {
		t.Errorf("daemon PID should change after reboot, still %v", daemonPIDBefore)
	}

	// Process PIDs should have changed (new processes)
	newPID := env.GetProcessField("rb1", "pid")
	if newPID == oldPID {
		t.Errorf("process PID should change after reboot, still %s", oldPID)
	}

	// Both processes should be present
	count := env.ProcessCount()
	if count != 2 {
		t.Errorf("expected 2 processes after reboot, got %d", count)
	}
}

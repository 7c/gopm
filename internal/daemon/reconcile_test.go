package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// spawnMarked launches sleep with GOPM_MANAGED_NAME=<name> in its own pgroup
// and returns the (pid, pgid, cleanup) plus the fingerprint that will match
// it. Different OSes match on different signals — the fingerprint packs
// both so a test can hand the same shape to findOrphans/reconcile
// regardless of OS.
//
// The sleep uses a per-test unique duration (which becomes argv[1]) so
// concurrent tests don't collide on Darwin's argv-based matcher.
func spawnMarked(t *testing.T, base string, setpgid bool) (int, int, func(), orphanFingerprint) {
	t.Helper()
	name, dur := uniqueFingerprint(t, base)
	sleepPath := sleepBinPath()
	cmd := exec.Command(sleepPath, dur)
	cmd.Env = append(os.Environ(),
		managedNameEnv+"="+name,
		managedIDEnv+"=999",
	)
	if setpgid {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn sleep with marker %q: %v", name, err)
	}
	pid := cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("getpgid(%d): %v", pid, err)
	}

	cleanup := func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = syscall.Kill(pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	}
	fp := orphanFingerprint{
		Name:    name,
		Command: sleepPath,
		Args:    []string{dur},
	}
	return pid, pgid, cleanup, fp
}

// spawnMarkedFork launches `sh -c "sleep A & sleep B & wait"` with the env
// marker and Setpgid=true. sh + both sleeps share one process group. For
// Darwin's argv matcher we identify the parent sh (its argv is "sh -c ...").
func spawnMarkedFork(t *testing.T, base string) (int, int, func(), orphanFingerprint) {
	t.Helper()
	name, _ := uniqueFingerprint(t, base)
	// Give each fork a distinct sleep duration so Darwin's argv matcher
	// can pick them up even without env visibility. The parent sh is
	// what the fingerprint's Command+Args identifies.
	script := "sleep 120 & sleep 121 & wait"
	shPath := "/bin/sh"
	scriptFlag := "-c"

	cmd := exec.Command(shPath, scriptFlag, script)
	cmd.Env = append(os.Environ(),
		managedNameEnv+"="+name,
		managedIDEnv+"=999",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn sh with marker %q: %v", name, err)
	}
	pid := cmd.Process.Pid

	fp := orphanFingerprint{
		Name:    name,
		Command: shPath,
		Args:    []string{scriptFlag, script},
	}

	// Wait for sh to actually fork the two sleeps so all three are visible.
	// We check for the sh process itself using its fingerprint; the sleeps
	// are collateral that share pgid.
	waitForOrphanCount(t, fp, 1, 2*time.Second)

	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("getpgid(%d): %v", pid, err)
	}

	cleanup := func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	}
	return pid, pgid, cleanup, fp
}

// waitForOrphanCount polls findOrphans until it returns exactly n PIDs.
func waitForOrphanCount(t *testing.T, fp orphanFingerprint, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		got := len(findOrphans(fp))
		if got == n {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waitForOrphanCount(%+v, %d) timed out after %s (last saw %d: %v)",
		fp, n, timeout, len(findOrphans(fp)), findOrphans(fp))
}

// uniqueFingerprint returns (env-name, sleep-duration) that are both
// distinctive enough to survive concurrent tests. Duration is used as
// argv[1] so Darwin's argv matcher can distinguish tests from each other
// AND from any unrelated `sleep 60` a developer might have running.
func uniqueFingerprint(t *testing.T, base string) (string, string) {
	t.Helper()
	// Duration string like "947000" (seconds) — big and unlikely to
	// collide with a human-typed sleep duration. Test cleanup kills the
	// process well before it would actually elapse.
	dur := fmt.Sprintf("%d", 900000+time.Now().UnixNano()%9000)
	name := fmt.Sprintf("gopm-test-%s-%d-%s", base, os.Getpid(), t.Name())
	return name, dur
}

// sleepBinPath returns an absolute path to sleep, so Darwin's argv[0]
// comparison finds the same string that exec.Command records.
func sleepBinPath() string {
	if _, err := os.Stat("/bin/sleep"); err == nil {
		return "/bin/sleep"
	}
	if p, err := exec.LookPath("sleep"); err == nil {
		return p
	}
	return "sleep"
}

func TestFindOrphans_Detects(t *testing.T) {
	pid, _, cleanup, fp := spawnMarked(t, "detect", true)
	defer cleanup()

	waitForOrphanCount(t, fp, 1, 2*time.Second)

	pids := findOrphans(fp)
	found := false
	for _, p := range pids {
		if p == pid {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("findOrphans(%+v) = %v, expected to contain PID %d",
			fp, pids, pid)
	}
}

func TestFindOrphans_IgnoresUnrelatedProcesses(t *testing.T) {
	// A plain sleep with no marker (Linux) or a distinct duration (Darwin)
	// must never match a fingerprint that doesn't describe it.
	cmd := exec.Command(sleepBinPath(), "77")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn plain sleep: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	fp := orphanFingerprint{
		Name:    "definitely-not-a-real-name-" + t.Name(),
		Command: sleepBinPath(),
		Args:    []string{"999999999"}, // duration nothing else will match
	}
	pids := findOrphans(fp)
	for _, p := range pids {
		if p == cmd.Process.Pid {
			t.Fatalf("plain sleep PID %d matched despite non-matching fingerprint", p)
		}
	}
}

func TestReconcileOrphans_KillsSinglePgroup(t *testing.T) {
	pid, _, cleanup, fp := spawnMarked(t, "single", true)
	defer cleanup()

	waitForOrphanCount(t, fp, 1, 2*time.Second)

	killed := reconcileOrphans(fp, 3*time.Second)
	if killed == 0 {
		t.Fatalf("reconcileOrphans killed 0 pgroups, expected at least 1")
	}

	waitForOrphanCount(t, fp, 0, 2*time.Second)

	if !isPidGone(pid) {
		t.Fatalf("PID %d still alive after reconcile", pid)
	}
}

func TestReconcileOrphans_KillsForkedChildren(t *testing.T) {
	// The C-style-fork case: shell forks two sleep children into its own
	// pgroup (non-interactive sh has job control off). One kill(-pgid)
	// takes all three down. We identify by the sh process fingerprint;
	// pgroup kill sweeps the sleeps regardless of their argv.
	shPid, pgid, cleanup, fp := spawnMarkedFork(t, "fork")
	defer cleanup()

	// Verify the two sleep children are actually running under the pgroup.
	sleepPids := findChildrenInPgroup(t, pgid)
	if len(sleepPids) < 2 {
		t.Fatalf("expected 2 sleep children in pgroup %d, saw %d: %v",
			pgid, len(sleepPids), sleepPids)
	}
	t.Logf("sh pid=%d pgid=%d, children=%v", shPid, pgid, sleepPids)

	killed := reconcileOrphans(fp, 3*time.Second)
	if killed == 0 {
		t.Fatalf("reconcileOrphans killed 0 pgroups")
	}

	// Give the OS time to reap.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		remaining := findChildrenInPgroup(t, pgid)
		if !pidAlive(shPid) && len(remaining) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	if pidAlive(shPid) {
		t.Errorf("sh PID %d survived reconcile", shPid)
	}
	for _, p := range sleepPids {
		if pidAlive(p) {
			t.Errorf("forked sleep PID %d survived reconcile", p)
		}
	}
}

func TestReconcileOrphans_KillsMultipleCopies(t *testing.T) {
	// Two separate processes with matching fingerprints, each in its own
	// pgroup — the "multiple copies from multiple prior daemon sessions"
	// case. Both pgroups must die.
	//
	// We spawn them under a common env-marker name so Linux picks them
	// both up, and give both the exact same argv so Darwin's argv matcher
	// picks them both up too.
	base := "multi"
	name, dur := uniqueFingerprint(t, base)
	sleepPath := sleepBinPath()

	spawn := func() (int, int, *exec.Cmd) {
		cmd := exec.Command(sleepPath, dur)
		cmd.Env = append(os.Environ(),
			managedNameEnv+"="+name,
			managedIDEnv+"=999",
		)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatalf("spawn copy: %v", err)
		}
		pgid, err := syscall.Getpgid(cmd.Process.Pid)
		if err != nil {
			t.Fatalf("getpgid: %v", err)
		}
		return cmd.Process.Pid, pgid, cmd
	}
	pid1, pgid1, cmd1 := spawn()
	defer func() { _ = syscall.Kill(-pgid1, syscall.SIGKILL); _, _ = cmd1.Process.Wait() }()
	pid2, pgid2, cmd2 := spawn()
	defer func() { _ = syscall.Kill(-pgid2, syscall.SIGKILL); _, _ = cmd2.Process.Wait() }()

	if pgid1 == pgid2 {
		t.Fatalf("test setup bug: both spawns landed in the same pgroup %d", pgid1)
	}

	fp := orphanFingerprint{Name: name, Command: sleepPath, Args: []string{dur}}
	waitForOrphanCount(t, fp, 2, 2*time.Second)

	killed := reconcileOrphans(fp, 3*time.Second)
	if killed < 2 {
		t.Fatalf("reconcileOrphans killed %d pgroups, expected 2", killed)
	}

	waitForOrphanCount(t, fp, 0, 2*time.Second)

	if !isPidGone(pid1) {
		t.Errorf("copy 1 (PID %d) survived", pid1)
	}
	if !isPidGone(pid2) {
		t.Errorf("copy 2 (PID %d) survived", pid2)
	}
}

func TestReconcileOrphans_NoopWhenNothingMatches(t *testing.T) {
	fp := orphanFingerprint{
		Name:    "nothing-matches-" + t.Name(),
		Command: sleepBinPath(),
		Args:    []string{"999999999"},
	}
	killed := reconcileOrphans(fp, 500*time.Millisecond)
	if killed != 0 {
		t.Fatalf("reconcileOrphans returned %d with no orphans present", killed)
	}
}

func TestReconcileOrphans_SurvivesTerm_UsesKill(t *testing.T) {
	// Trap SIGTERM in the child; reconcile must escalate to SIGKILL.
	// The bash loop re-arms sleep every time SIGTERM interrupts wait,
	// so SIGTERM alone doesn't kill the pgroup. SIGKILL is unblockable.
	if runtime.GOOS == "darwin" {
		// Darwin identifies by argv; bash -c '...' argv is very specific
		// and we can rely on it. On Linux the env-marker approach works
		// the same regardless of shell/script.
		if _, err := exec.LookPath("bash"); err != nil {
			t.Skip("bash not available")
		}
	}
	name, _ := uniqueFingerprint(t, "sigkill")
	scriptFlag := "-c"
	script := "trap 'echo trapped' TERM; while :; do sleep 60 & wait $!; done"
	bashPath := "/bin/bash"

	cmd := exec.Command(bashPath, scriptFlag, script)
	cmd.Env = append(os.Environ(),
		managedNameEnv+"="+name,
		managedIDEnv+"=999",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn trapping bash: %v", err)
	}
	pid := cmd.Process.Pid
	pgid, _ := syscall.Getpgid(pid)
	defer func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	}()

	fp := orphanFingerprint{
		Name:    name,
		Command: bashPath,
		Args:    []string{scriptFlag, script},
	}
	waitForOrphanCount(t, fp, 1, 2*time.Second)

	// Short kill_timeout forces the SIGKILL branch quickly.
	start := time.Now()
	killed := reconcileOrphans(fp, 500*time.Millisecond)
	elapsed := time.Since(start)

	if killed == 0 {
		t.Fatalf("reconcileOrphans killed 0 pgroups")
	}
	if elapsed < 500*time.Millisecond {
		t.Errorf("reconcile returned in %s, expected to wait at least the kill_timeout", elapsed)
	}

	waitForOrphanCount(t, fp, 0, 3*time.Second)

	if !isPidGone(pid) {
		t.Errorf("trapping bash (PID %d) survived reconcile", pid)
	}
}

// findChildrenInPgroup returns non-zombie PIDs whose current PGID equals
// pgid. Zombies are excluded because they still report a pgid even
// though the process is dead — including them would give false positives
// in "still alive after reconcile" assertions.
func findChildrenInPgroup(t *testing.T, pgid int) []int {
	t.Helper()
	entries, err := listAllPids()
	if err != nil {
		t.Fatalf("list pids: %v", err)
	}
	var out []int
	for _, pid := range entries {
		got, err := syscall.Getpgid(pid)
		if err != nil {
			continue
		}
		if got != pgid {
			continue
		}
		if isZombieLinux(pid) {
			continue
		}
		out = append(out, pid)
	}
	return out
}

func listAllPids() ([]int, error) {
	if runtime.GOOS == "linux" {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return nil, err
		}
		var out []int
		for _, e := range entries {
			if pid, err := strconv.Atoi(e.Name()); err == nil {
				out = append(out, pid)
			}
		}
		return out, nil
	}
	out, err := exec.Command("ps", "-A", "-o", "pid=").Output()
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, field := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(field); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// isPidGone reports true if the PID no longer exists. Zombies (dead but
// not yet reaped) are treated as gone: they occupy a PID slot but the
// process is not executing.
//
// Tests running here are the direct parent of the processes they spawn,
// so we try Wait4(WNOHANG) first to reap any zombie of pid. If Wait4
// succeeds (pid reaped) OR kill(pid, 0) returns ESRCH, pid is gone.
func isPidGone(pid int) bool {
	// Best-effort reap in case we're pid's parent.
	var status syscall.WaitStatus
	_, _ = syscall.Wait4(pid, &status, syscall.WNOHANG, nil)

	if err := syscall.Kill(pid, 0); err != nil {
		return err == syscall.ESRCH
	}
	// Kill returned nil — pid slot still exists. Could be a zombie of a
	// process whose parent is somebody else (we can't reap). Check state
	// via /proc on Linux; on Darwin assume alive (this code path is rare
	// in our tests since we're always the parent).
	return isZombieLinux(pid)
}

func pidAlive(pid int) bool {
	return !isPidGone(pid)
}

func TestEnvTokenMatches(t *testing.T) {
	cases := []struct {
		desc  string
		blob  string
		key   string
		value string
		sep   byte
		want  bool
	}{
		{"exact match, NUL sep", "FOO=bar\x00BAZ=qux", "FOO", "bar", 0, true},
		{"exact match, space sep", "FOO=bar BAZ=qux", "FOO", "bar", ' ', true},
		{"prefix collision must not match (NUL)", "FOO=bar-extra\x00", "FOO", "bar", 0, false},
		{"prefix collision must not match (space)", "FOO=bar-extra ", "FOO", "bar", ' ', false},
		{"substring collision (value inside another)", "FOO=xxbar", "FOO", "bar", 0, false},
		{"key at start of blob", "KEY=v\x00OTHER=z", "KEY", "v", 0, true},
		{"key at end of blob", "OTHER=z\x00KEY=v", "KEY", "v", 0, true},
		{"missing key", "OTHER=z\x00", "KEY", "v", 0, false},
		{"value with = inside", "KEY=a=b\x00", "KEY", "a=b", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := envTokenMatches([]byte(tc.blob), tc.key, tc.value, tc.sep)
			if got != tc.want {
				t.Errorf("envTokenMatches(%q,%q,%q,%q) = %v, want %v",
					tc.blob, tc.key, tc.value, string(tc.sep), got, tc.want)
			}
		})
	}
}

func TestArgvMatches(t *testing.T) {
	cases := []struct {
		desc    string
		argv    []string
		command string
		args    []string
		want    bool
	}{
		{"exact match", []string{"/bin/sleep", "10"}, "/bin/sleep", []string{"10"}, true},
		{"basename fallback", []string{"sleep", "10"}, "/bin/sleep", []string{"10"}, true},
		{"different args", []string{"/bin/sleep", "10"}, "/bin/sleep", []string{"20"}, false},
		{"extra args", []string{"/bin/sleep", "10", "20"}, "/bin/sleep", []string{"10"}, false},
		{"missing args", []string{"/bin/sleep"}, "/bin/sleep", []string{"10"}, false},
		{"empty argv", nil, "/bin/sleep", []string{"10"}, false},
		{"empty args, exact command", []string{"/bin/true"}, "/bin/true", nil, true},
		{"different command entirely", []string{"/bin/echo", "hi"}, "/bin/sleep", []string{"hi"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			if got := argvMatches(tc.argv, tc.command, tc.args); got != tc.want {
				t.Errorf("argvMatches(%v,%q,%v) = %v, want %v",
					tc.argv, tc.command, tc.args, got, tc.want)
			}
		})
	}
}

// TestFingerprintFor guards the state.go -> reconcile wiring: the fields
// state.go feeds in must match what findOrphans looks at.
func TestFingerprintFor(t *testing.T) {
	info := protocol.ProcessInfo{
		Name:    "api",
		Command: "/usr/bin/node",
		Args:    []string{"server.js", "--port", "3000"},
	}
	fp := fingerprintFor(info)
	if fp.Name != info.Name {
		t.Errorf("Name: got %q, want %q", fp.Name, info.Name)
	}
	if fp.Command != info.Command {
		t.Errorf("Command: got %q, want %q", fp.Command, info.Command)
	}
	if len(fp.Args) != len(info.Args) {
		t.Fatalf("Args length: got %d, want %d", len(fp.Args), len(info.Args))
	}
	for i, a := range info.Args {
		if fp.Args[i] != a {
			t.Errorf("Args[%d]: got %q, want %q", i, fp.Args[i], a)
		}
	}
}

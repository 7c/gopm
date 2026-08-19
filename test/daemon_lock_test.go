package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestDaemonLockPreventsSecondInstance is the regression test for the
// multi-daemon-race bug observed on 68.183.87.226: two `gopm --daemon`
// invocations in the same instant both booted before either had spawned
// children, both ran reconcile (finding nothing since neither had
// spawned yet), and both proceeded to spawn full sets of children —
// giving 2× copies of each managed process and permanent port conflicts.
//
// The flock at the top of daemon.Run() is the single-instance guard
// that closes this race. Second `--daemon` invocation must exit cleanly
// with a non-zero exit code or a distinct "already running" signal, and
// must NOT bind the socket or spawn any children.
func TestDaemonLockPreventsSecondInstance(t *testing.T) {
	env := NewTestEnv(t)

	// Bring up daemon #1 the normal way.
	env.MustGopm("ping")

	// Find and record daemon #1's PID from the PID file.
	pidPath := filepath.Join(env.Home, "daemon.pid")
	firstPID := readPIDFile(t, pidPath)
	if firstPID <= 0 {
		t.Fatalf("first daemon PID file missing or invalid: %d", firstPID)
	}
	t.Logf("first daemon PID: %d", firstPID)

	// Attempt to launch a second daemon directly. Without the flock this
	// second process would happily overwrite the PID file, race for the
	// socket, and start spawning children.
	second := exec.Command(env.GopmBin, "--daemon")
	second.Env = append(os.Environ(), "GOPM_HOME="+env.Home)
	// Detach from our test process the same way the CLI's auto-start
	// does, so this really is a peer daemon and not our child.
	second.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stderr strings.Builder
	second.Stderr = &stderr
	if err := second.Start(); err != nil {
		t.Fatalf("launch second daemon: %v", err)
	}

	// Wait for the second process to exit — it should exit fast because
	// the lock is held.
	done := make(chan error, 1)
	go func() { done <- second.Wait() }()
	select {
	case err := <-done:
		t.Logf("second daemon exited: err=%v stderr=%q", err, stderr.String())
	case <-time.After(3 * time.Second):
		_ = second.Process.Kill()
		t.Fatalf("second daemon did not exit within 3s — flock guard missing or broken\nstderr so far: %q", stderr.String())
	}

	// stderr should include the "already running" signal so an operator
	// launching --daemon by hand gets a clear diagnosis.
	if !strings.Contains(stderr.String(), "already running") {
		t.Errorf("second daemon stderr missing 'already running' message: %q", stderr.String())
	}

	// PID file must still point at daemon #1 — the second daemon must
	// never have gotten far enough to overwrite it.
	currentPID := readPIDFile(t, pidPath)
	if currentPID != firstPID {
		t.Fatalf("daemon.pid changed after second launch: %d → %d (second daemon ran past the guard)",
			firstPID, currentPID)
	}

	// Daemon #1 must still respond to RPC.
	env.MustGopm("ping")

	// Sanity: only one gopm --daemon process is alive under this GOPM_HOME.
	// We identify by process cwd/environment rather than by name because
	// the test binary itself matches "gopm" on some platforms.
	// Cross-check via ps + /proc/environ. Skipped on Darwin because SIP
	// hides env from cross-process reads; the assertions above (second
	// exited cleanly with the diagnostic, PID file unchanged, RPC still
	// responsive) are sufficient there.
	if runtime.GOOS == "linux" {
		count, matches := countDaemonsForHome(t, env.Home)
		if count != 1 {
			t.Fatalf("expected exactly 1 daemon alive for %s, found %d:\n%s",
				env.Home, count, strings.Join(matches, "\n"))
		}
	}
}

// TestDaemonLockReleasedOnExit — after a clean daemon shutdown, a fresh
// `gopm --daemon` must be able to acquire the lock and start. This is
// the "lock doesn't leak" check.
func TestDaemonLockReleasedOnExit(t *testing.T) {
	env := NewTestEnv(t)

	// Start, then kill, the first daemon.
	env.MustGopm("ping")
	env.MustGopm("kill")
	env.WaitForDaemonStopped(5 * time.Second)

	// A fresh CLI call must be able to bring the daemon up again.
	env.MustGopm("ping")
	pidPath := filepath.Join(env.Home, "daemon.pid")
	if pid := readPIDFile(t, pidPath); pid <= 0 {
		t.Fatalf("fresh daemon after kill: PID file missing or invalid (%d)", pid)
	}
}

func readPIDFile(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read %s: %v", path, err)
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return 0
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// countDaemonsForHome walks ps and returns the number (+ matching cmd
// lines for debug) of `gopm --daemon` processes whose GOPM_HOME env
// equals home. On Linux we use /proc/<pid>/environ for precise env
// matching. On Darwin env is hidden by SIP, so we fall back to matching
// the binary path — env.GopmBin is per-test since tests use a shared
// `test/bin/gopm` binary but each TestEnv has a unique $GOPM_HOME under
// /tmp/gp-XXXXXX, and only ONE test at a time should be spawning under
// that home.
func countDaemonsForHome(t *testing.T, home string) (int, []string) {
	t.Helper()
	out, err := exec.Command("ps", "-A", "-o", "pid=,command=").Output()
	if err != nil {
		t.Fatalf("ps -A: %v", err)
	}
	count := 0
	var matches []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sp := strings.IndexAny(line, " \t")
		if sp <= 0 {
			continue
		}
		cmd := strings.TrimSpace(line[sp+1:])
		if !strings.Contains(cmd, "gopm") || !strings.Contains(cmd, "--daemon") {
			continue
		}
		pid := readPIDFromStr(line[:sp])
		if pid <= 0 || pid == os.Getpid() {
			continue
		}
		// Linux: strict env match. Darwin: env hidden, skip if we can't
		// confirm — otherwise we'd count unrelated `gopm --daemon`
		// processes (e.g. an operator's real daemon on their dev machine).
		envHome, ok := readGopmHome(pid)
		if !ok {
			// Env unreadable — either dead process race, foreign uid,
			// or Darwin SIP. Do NOT count; the caller has other checks
			// (PID file, RPC ping) that already verify the invariant.
			continue
		}
		if envHome == home {
			count++
			matches = append(matches, "  "+line)
		}
	}
	return count, matches
}

func readPIDFromStr(s string) int {
	s = strings.TrimSpace(s)
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}

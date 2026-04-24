package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestLogsDefaultShowsBothStreams verifies that plain `gopm logs <name>`
// (no flags) returns both stdout and stderr content, each tagged with its
// stream marker, merged in chronological order. This is the default behavior.
func TestLogsDefaultShowsBothStreams(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin,
		"--name", "dual-default",
		"--",
		"--stdout-every", "50ms",
		"--stderr-every", "60ms",
		"--stdout-msg", "hello-stdout",
		"--stderr-msg", "hello-stderr",
	)
	env.WaitForStatus("dual-default", "online", 5*time.Second)
	time.Sleep(1200 * time.Millisecond)

	out := env.MustGopm("logs", "dual-default", "-n", "50")
	clean := stripANSI(out)

	if !strings.Contains(clean, "[OUT]") {
		t.Errorf("default logs missing [OUT] marker:\n%s", clean)
	}
	if !strings.Contains(clean, "[ERR]") {
		t.Errorf("default logs missing [ERR] marker:\n%s", clean)
	}
	if !strings.Contains(clean, "hello-stdout") {
		t.Errorf("default logs missing stdout body:\n%s", clean)
	}
	if !strings.Contains(clean, "hello-stderr") {
		t.Errorf("default logs missing stderr body:\n%s", clean)
	}
}

// TestLogsErrFlagStderrOnly verifies `gopm logs --err` returns stderr only,
// and never includes stdout content.
func TestLogsErrFlagStderrOnly(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin,
		"--name", "erronly",
		"--",
		"--stdout-every", "50ms",
		"--stderr-every", "60ms",
		"--stdout-msg", "only-out",
		"--stderr-msg", "only-err",
	)
	env.WaitForStatus("erronly", "online", 5*time.Second)
	time.Sleep(1000 * time.Millisecond)

	out := env.MustGopm("logs", "erronly", "--err", "-n", "50")
	clean := stripANSI(out)

	if !strings.Contains(clean, "only-err") {
		t.Errorf("--err output missing stderr body:\n%s", clean)
	}
	if strings.Contains(clean, "only-out") {
		t.Errorf("--err output must not include stdout body:\n%s", clean)
	}
	// --err uses single-stream flow, no [OUT]/[ERR] markers.
	if strings.Contains(clean, "[OUT]") {
		t.Errorf("--err output should not have [OUT] marker:\n%s", clean)
	}
}

// TestLogsAllFlag verifies `gopm logs -a` (kept as compatibility alias)
// returns both stdout and stderr content, each tagged with its stream marker,
// merged in chronological order.
func TestLogsAllFlag(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin,
		"--name", "dual",
		"--",
		"--stdout-every", "50ms",
		"--stderr-every", "60ms",
		"--stdout-msg", "hello-stdout",
		"--stderr-msg", "hello-stderr",
	)
	env.WaitForStatus("dual", "online", 5*time.Second)

	// Give the process enough time to emit several lines on both streams.
	time.Sleep(1200 * time.Millisecond)

	out := env.MustGopm("logs", "dual", "-a", "-n", "50")

	// Strip ANSI escapes so we can reliably grep.
	clean := stripANSI(out)

	if !strings.Contains(clean, "[OUT]") {
		t.Errorf("logs -a missing [OUT] marker:\n%s", clean)
	}
	if !strings.Contains(clean, "[ERR]") {
		t.Errorf("logs -a missing [ERR] marker:\n%s", clean)
	}
	if !strings.Contains(clean, "hello-stdout") {
		t.Errorf("logs -a missing stdout body:\n%s", clean)
	}
	if !strings.Contains(clean, "hello-stderr") {
		t.Errorf("logs -a missing stderr body:\n%s", clean)
	}

	// Every [OUT] line must carry "hello-stdout" and never "hello-stderr".
	// Same for [ERR]. Validates that merging preserves stream identity.
	for _, line := range strings.Split(clean, "\n") {
		if strings.Contains(line, "[OUT]") {
			if strings.Contains(line, "hello-stderr") {
				t.Errorf("stderr content under [OUT] marker:\n%s", line)
			}
		}
		if strings.Contains(line, "[ERR]") {
			if strings.Contains(line, "hello-stdout") {
				t.Errorf("stdout content under [ERR] marker:\n%s", line)
			}
		}
	}
}

// TestLogsAllFlagOverridesErr verifies -a takes precedence over --err.
// When both are passed, we should still see BOTH streams.
func TestLogsAllFlagOverridesErr(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin,
		"--name", "mixed",
		"--",
		"--stdout-every", "50ms",
		"--stderr-every", "60ms",
		"--stdout-msg", "mix-out",
		"--stderr-msg", "mix-err",
	)
	env.WaitForStatus("mixed", "online", 5*time.Second)
	time.Sleep(800 * time.Millisecond)

	out := env.MustGopm("logs", "mixed", "-a", "--err", "-n", "50")
	clean := stripANSI(out)
	if !strings.Contains(clean, "mix-out") || !strings.Contains(clean, "mix-err") {
		t.Errorf("logs -a --err should include both streams:\n%s", clean)
	}
}

// TestLogsFollowSurvivesRotation is a regression test for the bug where
// `gopm logs -f` froze shortly after the log file rotated (the follower held
// a stale FD to the renamed file). We flood stdout with a tiny rotation
// threshold so rotation happens within a second, then assert the follower
// kept emitting lines across multiple rotations.
func TestLogsFollowSurvivesRotation(t *testing.T) {
	env := NewTestEnv(t)

	env.MustGopm("start", env.TestappBin,
		"--name", "rotater",
		"--max-log-size", "20K",
		"--",
		"--stdout-flood", "--stdout-size", "200",
	)
	env.WaitForStatus("rotater", "online", 5*time.Second)
	// Let the process burn through several rotations before we start following.
	time.Sleep(500 * time.Millisecond)

	cmd := exec.Command(env.GopmBin, "logs", "rotater", "-f", "-n", "3")
	cmd.Env = append(os.Environ(), "GOPM_HOME="+env.Home)
	var outBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	// 2.5s at ~200-byte lines + 20K rotation threshold guarantees several
	// rotations. A working follower will stream tens of thousands of lines;
	// the buggy one freezes within milliseconds and stops at the initial dump.
	time.Sleep(2500 * time.Millisecond)
	_ = cmd.Process.Signal(syscall.SIGTERM)
	_ = cmd.Wait()

	// Confirm rotation actually happened during the test window.
	rotatedPath := filepath.Join(env.Home, "logs", "rotater-out.log.1")
	if _, err := os.Stat(rotatedPath); err != nil {
		entries, _ := os.ReadDir(filepath.Join(env.Home, "logs"))
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected rotation at %s: %v\nlogs dir contents: %v", rotatedPath, err, names)
	}

	lineCount := strings.Count(outBuf.String(), "\n")
	// Threshold chosen well above any plausible "froze after initial dump"
	// outcome (which would be ~3 lines) and well below what a healthy follower
	// produces at this rate (~thousands). 100 is a comfortable middle ground.
	if lineCount < 100 {
		preview := outBuf.String()
		if len(preview) > 2000 {
			preview = preview[:2000] + "\n...[truncated]"
		}
		t.Errorf("follower froze after rotation: only %d lines in 2.5s\noutput:\n%s", lineCount, preview)
	}
}

// stripANSI removes ANSI color/style escape sequences from a string.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

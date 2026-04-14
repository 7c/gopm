package test

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestLogsAllFlag verifies `gopm logs -a` returns both stdout and stderr
// content, each tagged with its stream marker, merged in chronological order.
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

// stripANSI removes ANSI color/style escape sequences from a string.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

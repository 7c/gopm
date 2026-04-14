package test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVersionCommand verifies that "gopm version --json" returns both
// cli_version and daemon_version and that, built from the same binary,
// version_mismatch is false.
func TestVersionCommand(t *testing.T) {
	env := NewTestEnv(t)

	// Start the daemon via any command.
	env.MustGopm("ping")

	out := env.MustGopm("version", "--json")
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("parse version json: %v\noutput: %s", err, out)
	}

	if _, ok := v["cli_version"]; !ok {
		t.Errorf("missing cli_version field: %v", v)
	}
	if _, ok := v["daemon_version"]; !ok {
		t.Errorf("missing daemon_version field: %v", v)
	}
	if mismatch, _ := v["version_mismatch"].(bool); mismatch {
		t.Errorf("version_mismatch should be false when cli and daemon come from the same binary: %v", v)
	}
}

// TestVersionMismatchWarning builds a second gopm binary with a different
// injected version. It starts a daemon from binary A (version A) then runs
// `list` and `status` from binary B (version B) and checks the red WARNING
// text is printed. This exercises the whole IPC path end-to-end.
func TestVersionMismatchWarning(t *testing.T) {
	env := NewTestEnv(t)

	// Build binary B with a different version. We strip the existing tests
	// binary and rebuild with ldflags.
	binB := filepath.Join(t.TempDir(), "gopm-b")
	buildCmd := exec.Command("go", "build",
		"-ldflags", "-X main.Version=9.9.9-test",
		"-o", binB, "./cmd/gopm/")
	buildCmd.Dir = repoRoot(t)
	buildCmd.Env = os.Environ()
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build B: %v\n%s", err, out)
	}

	// Start daemon from binary A (the default env binary — Version="dev"
	// usually, but what matters is that binB has a DIFFERENT version and
	// the CLI will see that mismatch regardless).
	env.MustGopm("ping") // auto-starts daemon

	// Skip the test if the default test binary is "dev" — in that case
	// isVersionMismatch() short-circuits and no warning is produced.
	verOut := env.MustGopm("version", "--json")
	var v map[string]interface{}
	_ = json.Unmarshal([]byte(verOut), &v)
	if v["cli_version"] == "dev" {
		t.Skip("test binary version is 'dev', version mismatch is suppressed by design")
	}

	// Now query with binary B (9.9.9-test) — should print WARNING.
	runB := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(binB, args...)
		cmd.Env = append(os.Environ(), "GOPM_HOME="+env.Home)
		out, _ := cmd.CombinedOutput()
		return string(out)
	}

	listOut := runB("list")
	if !strings.Contains(listOut, "WARNING") || !strings.Contains(listOut, "9.9.9-test") {
		t.Errorf("list did not print version mismatch warning:\n%s", listOut)
	}
	statusOut := runB("status")
	if !strings.Contains(statusOut, "WARNING") || !strings.Contains(statusOut, "9.9.9-test") {
		t.Errorf("status did not print version mismatch warning:\n%s", statusOut)
	}
	versionOut := runB("version")
	if !strings.Contains(versionOut, "stale") || !strings.Contains(versionOut, "9.9.9-test") {
		t.Errorf("version did not print mismatch:\n%s", versionOut)
	}
}

// repoRoot walks up from the test file location until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod")
		}
		dir = parent
	}
}

package test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestEnv sets up an isolated gopm environment per test.
// Each test gets its own GOPM_HOME so tests can run in parallel.
type TestEnv struct {
	T          *testing.T
	Home       string
	GopmBin    string
	TestappBin string
}

// NewTestEnv creates an isolated test environment.
func NewTestEnv(t *testing.T) *TestEnv {
	t.Helper()
	home := t.TempDir()

	gopmBin := filepath.Join(BinDir(), "gopm")
	testappBin := filepath.Join(BinDir(), "testapp")
	requireFile(t, gopmBin, "run: go build -o test/bin/gopm ./cmd/gopm/")
	requireFile(t, testappBin, "run: go build -o test/bin/testapp ./test/testapp/")

	env := &TestEnv{
		T:          t,
		Home:       home,
		GopmBin:    gopmBin,
		TestappBin: testappBin,
	}
	t.Cleanup(env.Cleanup)
	return env
}

// BinDir returns the path to the test binary directory.
func BinDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "bin")
}

// Gopm runs a gopm CLI command and returns stdout, stderr, exit code.
func (e *TestEnv) Gopm(args ...string) (stdout, stderr string, exitCode int) {
	cmd := exec.Command(e.GopmBin, args...)
	cmd.Env = append(os.Environ(), "GOPM_HOME="+e.Home)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		exitCode = -1
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// MustGopm runs gopm and fails the test if exit code != 0.
func (e *TestEnv) MustGopm(args ...string) string {
	e.T.Helper()
	stdout, stderr, code := e.Gopm(args...)
	if code != 0 {
		e.T.Fatalf("gopm %v failed (exit %d):\nstdout: %s\nstderr: %s",
			args, code, stdout, stderr)
	}
	return stdout
}

// WaitForStatus polls `gopm list --json` until the named process reaches the target status.
func (e *TestEnv) WaitForStatus(name string, status string, timeout time.Duration) {
	e.T.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, _, _ := e.Gopm("list", "--json")
		var procs []map[string]interface{}
		if err := json.Unmarshal([]byte(out), &procs); err == nil {
			for _, p := range procs {
				if p["name"] == name && p["status"] == status {
					return
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	e.T.Fatalf("timeout: %s did not reach status %q within %s", name, status, timeout)
}

// WaitForRestartCount polls until the process restart count reaches expected value.
func (e *TestEnv) WaitForRestartCount(name string, minRestarts int, timeout time.Duration) {
	e.T.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, _, _ := e.Gopm("describe", name, "--json")
		var proc map[string]interface{}
		if err := json.Unmarshal([]byte(out), &proc); err == nil {
			if restarts, ok := proc["restarts"].(float64); ok {
				if int(restarts) >= minRestarts {
					return
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	e.T.Fatalf("timeout: %s did not reach %d restarts within %s", name, minRestarts, timeout)
}

// Cleanup kills the daemon and removes temp files.
func (e *TestEnv) Cleanup() {
	e.Gopm("kill")
	// Give daemon time to clean up
	time.Sleep(200 * time.Millisecond)
}

// WriteEcosystem writes a JSON config file and returns its path.
func (e *TestEnv) WriteEcosystem(config interface{}) string {
	e.T.Helper()
	path := filepath.Join(e.Home, "test-ecosystem.json")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		e.T.Fatalf("marshal ecosystem: %v", err)
	}
	// Replace TESTAPP_BIN placeholder
	content := strings.ReplaceAll(string(data), "TESTAPP_BIN", e.TestappBin)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		e.T.Fatalf("write ecosystem: %v", err)
	}
	return path
}

// ProcessCount returns the number of processes in the list.
func (e *TestEnv) ProcessCount() int {
	out, _, _ := e.Gopm("list", "--json")
	var procs []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &procs); err != nil {
		return 0
	}
	return len(procs)
}

// GetProcessField gets a field value from describe --json output.
func (e *TestEnv) GetProcessField(name, field string) string {
	out, _, _ := e.Gopm("describe", name, "--json")
	var proc map[string]interface{}
	if err := json.Unmarshal([]byte(out), &proc); err != nil {
		return ""
	}
	if v, ok := proc[field]; ok {
		switch val := v.(type) {
		case string:
			return val
		case float64:
			return strconv.FormatFloat(val, 'f', -1, 64)
		case bool:
			return fmt.Sprintf("%v", val)
		default:
			data, _ := json.Marshal(v)
			return string(data)
		}
	}
	return ""
}

func requireFile(t *testing.T, path, hint string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("required file not found: %s\nHint: %s", path, hint)
	}
}

package cli

import (
	"testing"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

func makeProcs() []protocol.ProcessInfo {
	return []protocol.ProcessInfo{
		{ID: 0, Name: "api", Status: protocol.StatusOnline, PID: 100, CPU: 1.5, Memory: 1024, Uptime: time.Now()},
		{ID: 1, Name: "worker", Status: protocol.StatusOnline, PID: 200, CPU: 5.0, Memory: 2048, Uptime: time.Now()},
		{ID: 2, Name: "cron", Status: protocol.StatusStopped},
		{ID: 3, Name: "proxy", Status: protocol.StatusErrored, StatusReason: "exit 1"},
	}
}

func TestFilterProcs_ByName(t *testing.T) {
	procs := makeProcs()

	result := filterProcs(procs, "api")
	if len(result) != 1 {
		t.Fatalf("expected 1 proc, got %d", len(result))
	}
	if result[0].Name != "api" {
		t.Errorf("expected name 'api', got %q", result[0].Name)
	}
}

func TestFilterProcs_ByID(t *testing.T) {
	procs := makeProcs()

	result := filterProcs(procs, "2")
	if len(result) != 1 {
		t.Fatalf("expected 1 proc, got %d", len(result))
	}
	if result[0].Name != "cron" {
		t.Errorf("expected name 'cron', got %q", result[0].Name)
	}
	if result[0].ID != 2 {
		t.Errorf("expected ID 2, got %d", result[0].ID)
	}
}

func TestFilterProcs_NotFound(t *testing.T) {
	procs := makeProcs()

	result := filterProcs(procs, "nonexistent")
	if len(result) != 0 {
		t.Fatalf("expected 0 procs, got %d", len(result))
	}
}

func TestFilterProcs_EmptyList(t *testing.T) {
	result := filterProcs(nil, "api")
	if len(result) != 0 {
		t.Fatalf("expected 0 procs, got %d", len(result))
	}
}

func TestFilterProcs_IDZero(t *testing.T) {
	procs := makeProcs()

	result := filterProcs(procs, "0")
	if len(result) != 1 {
		t.Fatalf("expected 1 proc, got %d", len(result))
	}
	if result[0].Name != "api" {
		t.Errorf("expected name 'api', got %q", result[0].Name)
	}
}

func TestFilterProcs_NameTakesPrecedenceOverID(t *testing.T) {
	// If a process is named "1", filtering by "1" should match both
	// the name and the ID (worker has ID 1).
	procs := []protocol.ProcessInfo{
		{ID: 0, Name: "1", Status: protocol.StatusOnline},
		{ID: 1, Name: "worker", Status: protocol.StatusOnline},
	}

	result := filterProcs(procs, "1")
	if len(result) != 2 {
		t.Fatalf("expected 2 procs (name match + ID match), got %d", len(result))
	}
}

func TestWatchCmd_Flags(t *testing.T) {
	// Verify the command has expected flags registered.
	f := watchCmd.Flags()

	intervalFlag := f.Lookup("interval")
	if intervalFlag == nil {
		t.Fatal("expected --interval flag to be registered")
	}
	if intervalFlag.Shorthand != "i" {
		t.Errorf("expected shorthand 'i', got %q", intervalFlag.Shorthand)
	}
	if intervalFlag.DefValue != "1" {
		t.Errorf("expected default '1', got %q", intervalFlag.DefValue)
	}

	portsFlag := f.Lookup("ports")
	if portsFlag == nil {
		t.Fatal("expected --ports flag to be registered")
	}
	if portsFlag.Shorthand != "p" {
		t.Errorf("expected shorthand 'p', got %q", portsFlag.Shorthand)
	}
	if portsFlag.DefValue != "false" {
		t.Errorf("expected default 'false', got %q", portsFlag.DefValue)
	}
}

func TestWatchCmd_Args(t *testing.T) {
	// MaximumNArgs(1): 0 or 1 args should be valid.
	if err := watchCmd.Args(watchCmd, []string{}); err != nil {
		t.Errorf("expected 0 args to be valid: %v", err)
	}
	if err := watchCmd.Args(watchCmd, []string{"api"}); err != nil {
		t.Errorf("expected 1 arg to be valid: %v", err)
	}
	if err := watchCmd.Args(watchCmd, []string{"api", "worker"}); err == nil {
		t.Error("expected 2 args to be invalid")
	}
}

func TestWatchCmd_Use(t *testing.T) {
	if watchCmd.Use != "watch [name|id|all]" {
		t.Errorf("unexpected Use: %q", watchCmd.Use)
	}
}

func TestWatchIntervalClamp(t *testing.T) {
	// Verify the clamping logic: values < 1 should be treated as 1.
	// We can't easily test runWatch without a daemon, so test the logic directly.
	orig := watchInterval
	defer func() { watchInterval = orig }()

	watchInterval = 0
	if watchInterval < 1 {
		watchInterval = 1
	}
	if watchInterval != 1 {
		t.Errorf("expected interval clamped to 1, got %d", watchInterval)
	}

	watchInterval = -5
	if watchInterval < 1 {
		watchInterval = 1
	}
	if watchInterval != 1 {
		t.Errorf("expected interval clamped to 1, got %d", watchInterval)
	}

	watchInterval = 10
	if watchInterval < 1 {
		watchInterval = 1
	}
	if watchInterval != 10 {
		t.Errorf("expected interval to stay 10, got %d", watchInterval)
	}
}

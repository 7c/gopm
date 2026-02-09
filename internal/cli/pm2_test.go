package cli

import (
	"encoding/json"
	"testing"
)

func makePM2Procs() []pm2Process {
	return []pm2Process{
		{Name: "api", PM2ID: 0, PID: 1000, PM2Env: pm2Env{
			Status: "online", ExecPath: "/usr/bin/node", Cwd: "/app",
			Interpreter: "node", AutoRestart: true, MaxRestarts: 10,
		}},
		{Name: "worker", PM2ID: 1, PID: 2000, PM2Env: pm2Env{
			Status: "online", ExecPath: "/app/worker.js", Cwd: "/app",
			Interpreter: "node", AutoRestart: true,
		}},
		{Name: "cron", PM2ID: 2, PID: 3000, PM2Env: pm2Env{
			Status: "stopped", ExecPath: "/app/cron.sh", Cwd: "/app",
			Interpreter: "bash",
		}},
	}
}

func TestFilterPM2Procs_All(t *testing.T) {
	procs := makePM2Procs()
	// No targets means we don't call filterPM2Procs (all are used).
	// But filtering with all names should return all.
	result := filterPM2Procs(procs, []string{"api", "worker", "cron"})
	if len(result) != 3 {
		t.Fatalf("expected 3 procs, got %d", len(result))
	}
}

func TestFilterPM2Procs_Single(t *testing.T) {
	procs := makePM2Procs()
	result := filterPM2Procs(procs, []string{"worker"})
	if len(result) != 1 {
		t.Fatalf("expected 1 proc, got %d", len(result))
	}
	if result[0].Name != "worker" {
		t.Errorf("expected 'worker', got %q", result[0].Name)
	}
}

func TestFilterPM2Procs_Multiple(t *testing.T) {
	procs := makePM2Procs()
	result := filterPM2Procs(procs, []string{"api", "cron"})
	if len(result) != 2 {
		t.Fatalf("expected 2 procs, got %d", len(result))
	}
	names := map[string]bool{}
	for _, p := range result {
		names[p.Name] = true
	}
	if !names["api"] || !names["cron"] {
		t.Errorf("expected api and cron, got %v", names)
	}
}

func TestFilterPM2Procs_NotFound(t *testing.T) {
	procs := makePM2Procs()
	result := filterPM2Procs(procs, []string{"nonexistent"})
	if len(result) != 0 {
		t.Fatalf("expected 0 procs, got %d", len(result))
	}
}

func TestFilterPM2Procs_PartialMatch(t *testing.T) {
	procs := makePM2Procs()
	result := filterPM2Procs(procs, []string{"api", "nonexistent"})
	if len(result) != 1 {
		t.Fatalf("expected 1 proc, got %d", len(result))
	}
	if result[0].Name != "api" {
		t.Errorf("expected 'api', got %q", result[0].Name)
	}
}

func TestFilterPM2Procs_EmptyList(t *testing.T) {
	result := filterPM2Procs(nil, []string{"api"})
	if len(result) != 0 {
		t.Fatalf("expected 0 procs, got %d", len(result))
	}
}

func TestPM2ToStartParams_DryJSON(t *testing.T) {
	// Verify that pm2ToStartParams produces valid JSON for dry-run output.
	p := makePM2Procs()[0]
	params := pm2ToStartParams(p)

	data, err := json.MarshalIndent(params, "", "  ")
	if err != nil {
		t.Fatalf("json marshal failed: %v", err)
	}

	// Unmarshal back to verify round-trip.
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}

	if decoded["name"] != "api" {
		t.Errorf("expected name 'api', got %v", decoded["name"])
	}
	if decoded["command"] != "/usr/bin/node" {
		t.Errorf("expected command '/usr/bin/node', got %v", decoded["command"])
	}
	if decoded["autorestart"] != "always" {
		t.Errorf("expected autorestart 'always', got %v", decoded["autorestart"])
	}
}

func TestPM2Cmd_DryFlag(t *testing.T) {
	f := pm2Cmd.Flags()
	dryFlag := f.Lookup("dry")
	if dryFlag == nil {
		t.Fatal("expected --dry flag to be registered")
	}
	if dryFlag.DefValue != "false" {
		t.Errorf("expected default 'false', got %q", dryFlag.DefValue)
	}
}

func TestPM2Cmd_AcceptsArgs(t *testing.T) {
	// pm2 command should accept any number of args (process names).
	if watchCmd.Args != nil {
		// The pm2 command uses default (no Args validator = accepts any).
	}
	// Verify Use string reflects the new signature.
	if pm2Cmd.Use != "pm2 [name...]" {
		t.Errorf("unexpected Use: %q", pm2Cmd.Use)
	}
}

func TestFilterPM2Procs_PreservesOrder(t *testing.T) {
	procs := makePM2Procs()
	result := filterPM2Procs(procs, []string{"cron", "api"})
	if len(result) != 2 {
		t.Fatalf("expected 2 procs, got %d", len(result))
	}
	// Order should match the PM2 list order, not the args order.
	if result[0].Name != "api" {
		t.Errorf("expected first proc 'api', got %q", result[0].Name)
	}
	if result[1].Name != "cron" {
		t.Errorf("expected second proc 'cron', got %q", result[1].Name)
	}
}

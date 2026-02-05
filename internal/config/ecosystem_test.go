package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEcosystem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eco.json")
	os.WriteFile(path, []byte(`{
		"apps": [
			{"name": "app1", "command": "/bin/echo", "args": ["hello"]},
			{"name": "app2", "command": "/bin/sleep", "args": ["10"]}
		]
	}`), 0644)

	eco, err := LoadEcosystem(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(eco.Apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(eco.Apps))
	}
	if eco.Apps[0].Name != "app1" {
		t.Errorf("app[0].Name = %q, want %q", eco.Apps[0].Name, "app1")
	}
	if eco.Apps[1].Command != "/bin/sleep" {
		t.Errorf("app[1].Command = %q, want %q", eco.Apps[1].Command, "/bin/sleep")
	}
}

func TestLoadEcosystemValidation(t *testing.T) {
	dir := t.TempDir()

	// Missing command
	path := filepath.Join(dir, "bad1.json")
	os.WriteFile(path, []byte(`{"apps":[{"name":"no-cmd"}]}`), 0644)
	_, err := LoadEcosystem(path)
	if err == nil {
		t.Error("expected validation error for missing command")
	}

	// Missing name
	path2 := filepath.Join(dir, "bad2.json")
	os.WriteFile(path2, []byte(`{"apps":[{"command":"/bin/echo"}]}`), 0644)
	_, err = LoadEcosystem(path2)
	if err == nil {
		t.Error("expected validation error for missing name")
	}

	// Duplicate names
	path3 := filepath.Join(dir, "bad3.json")
	os.WriteFile(path3, []byte(`{"apps":[
		{"name":"dup","command":"/bin/a"},
		{"name":"dup","command":"/bin/b"}
	]}`), 0644)
	_, err = LoadEcosystem(path3)
	if err == nil {
		t.Error("expected validation error for duplicate names")
	}

	// Empty apps
	path4 := filepath.Join(dir, "bad4.json")
	os.WriteFile(path4, []byte(`{"apps":[]}`), 0644)
	_, err = LoadEcosystem(path4)
	if err == nil {
		t.Error("expected validation error for empty apps")
	}
}

func TestAppConfigToStartParams(t *testing.T) {
	app := AppConfig{
		Name:         "myapp",
		Command:      "/usr/bin/node",
		Args:         []string{"server.js"},
		Cwd:          "/srv",
		AutoRestart:  "on-failure",
		MaxRestarts:  intPtr(5),
		RestartDelay: "2s",
	}

	params := app.ToStartParams()
	if params.Name != "myapp" {
		t.Errorf("Name = %q, want %q", params.Name, "myapp")
	}
	if params.Command != "/usr/bin/node" {
		t.Errorf("Command = %q", params.Command)
	}
	if params.AutoRestart != "on-failure" {
		t.Errorf("AutoRestart = %q", params.AutoRestart)
	}
	if params.MaxRestarts == nil || *params.MaxRestarts != 5 {
		t.Errorf("MaxRestarts = %v", params.MaxRestarts)
	}
	if params.RestartDelay != "2s" {
		t.Errorf("RestartDelay = %q", params.RestartDelay)
	}
}

func intPtr(i int) *int { return &i }

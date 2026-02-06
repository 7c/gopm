package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		err   bool
	}{
		{"1M", 1024 * 1024, false},
		{"10M", 10 * 1024 * 1024, false},
		{"1G", 1024 * 1024 * 1024, false},
		{"500K", 500 * 1024, false},
		{"1024", 1024, false},
		{"", 1048576, false}, // default 1MB
		{"abc", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseSize(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("ParseSize(%q) expected error, got %d", tt.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSize(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseSize(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input time.Duration
		want  string
	}{
		{0, "0s"},
		{500 * time.Millisecond, "0s"},
		{5 * time.Second, "5s"},
		{65 * time.Second, "1m"},
		{3661 * time.Second, "1h 1m"},
		{90061 * time.Second, "1d 1h 1m"},
	}
	for _, tt := range tests {
		got := FormatDuration(tt.input)
		if got != tt.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1536 * 1024, "1.5 MB"},
	}
	for _, tt := range tests {
		got := FormatBytes(tt.input)
		if got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDurationJSON(t *testing.T) {
	d := Duration{5 * time.Second}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"5s"` {
		t.Errorf("Duration marshal = %s, want %q", data, "5s")
	}

	var d2 Duration
	if err := json.Unmarshal(data, &d2); err != nil {
		t.Fatal(err)
	}
	if d2.Duration != 5*time.Second {
		t.Errorf("Duration unmarshal = %v, want 5s", d2.Duration)
	}

	// Test empty string
	var d3 Duration
	if err := json.Unmarshal([]byte(`""`), &d3); err != nil {
		t.Fatal(err)
	}
	if d3.Duration != 0 {
		t.Errorf("Duration unmarshal empty = %v, want 0", d3.Duration)
	}
}

func TestDefaultRestartPolicy(t *testing.T) {
	p := DefaultRestartPolicy()
	if p.AutoRestart != RestartAlways {
		t.Errorf("AutoRestart = %q, want %q", p.AutoRestart, RestartAlways)
	}
	if p.MaxRestarts != 0 {
		t.Errorf("MaxRestarts = %d, want 0 (unlimited)", p.MaxRestarts)
	}
	if p.KillSignal != 15 {
		t.Errorf("KillSignal = %d, want 15", p.KillSignal)
	}
}

func TestGopmHome(t *testing.T) {
	t.Setenv("GOPM_HOME", "/tmp/test-gopm")
	if got := GopmHome(); got != "/tmp/test-gopm" {
		t.Errorf("GopmHome() = %q, want /tmp/test-gopm", got)
	}
}

package cli

import (
	"strings"
	"testing"
)

func TestIsVersionMismatch(t *testing.T) {
	cases := []struct {
		name    string
		cli     string
		daemon  string
		want    bool
	}{
		{"both empty", "", "", false},
		{"cli dev", "dev", "0.0.35", false},
		{"cli empty", "", "0.0.35", false},
		{"daemon empty", "0.0.36", "", false},
		{"same version", "0.0.36", "0.0.36", false},
		{"mismatch", "0.0.36", "0.0.34", true},
		{"mismatch reversed", "0.0.34", "0.0.36", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isVersionMismatch(c.cli, c.daemon)
			if got != c.want {
				t.Errorf("isVersionMismatch(%q, %q) = %v, want %v",
					c.cli, c.daemon, got, c.want)
			}
		})
	}
}

func TestFormatVersionMismatchWarning(t *testing.T) {
	msg := formatVersionMismatchWarning("0.0.36", "0.0.34")
	if !strings.Contains(msg, "0.0.36") || !strings.Contains(msg, "0.0.34") {
		t.Errorf("warning missing versions: %q", msg)
	}
	if !strings.Contains(msg, "WARNING") {
		t.Errorf("warning missing WARNING prefix: %q", msg)
	}
	if !strings.Contains(msg, "gopm reboot") {
		t.Errorf("warning missing remediation hint: %q", msg)
	}
}

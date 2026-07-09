package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// renderRootHelp builds the root command and captures `gopm --help`. Executing
// with --help is what makes cobra attach its generated help and completion
// subcommands, so the result reflects the real help output.
func renderRootHelp(t *testing.T) string {
	t.Helper()
	return renderHelp(t, newRootCommand())
}

func renderHelp(t *testing.T, root *cobra.Command) string {
	t.Helper()

	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("rendering root help: %v", err)
	}
	return buf.String()
}

func TestRootHelpRendersGroupSections(t *testing.T) {
	out := renderRootHelp(t)

	for _, title := range []string{
		"Process Management:",
		"Daemon Management:",
		"Configuration & State:",
		"Tools & Diagnostics:",
	} {
		if !strings.Contains(out, title) {
			t.Errorf("help is missing the %q section:\n%s", title, out)
		}
	}

	// The flat fallback must not appear once groups exist.
	if strings.Contains(out, "Available Commands:") {
		t.Error("help rendered the ungrouped 'Available Commands:' fallback")
	}
	// Every command is assigned a group, so this bucket should stay empty.
	if strings.Contains(out, "Additional Commands:") {
		t.Errorf("help rendered 'Additional Commands:', meaning some command lacks a GroupID:\n%s", out)
	}
}

func TestRootHelpPlacesCommandsInTheirSection(t *testing.T) {
	out := renderRootHelp(t)

	// section returns the help text between a group title and the next one.
	section := func(title string) string {
		start := strings.Index(out, title)
		if start == -1 {
			t.Fatalf("section %q not found in help:\n%s", title, out)
		}
		rest := out[start+len(title):]
		// Sections are separated by a blank line before the next title.
		if end := strings.Index(rest, "Management:"); end != -1 {
			return rest[:end]
		}
		if end := strings.Index(rest, "Flags:"); end != -1 {
			return rest[:end]
		}
		return rest
	}

	cases := []struct{ title, command string }{
		{"Process Management:", "isprocess"},
		{"Process Management:", "isrunning"},
		{"Process Management:", "start"},
		{"Daemon Management:", "ping"},
		{"Configuration & State:", "import"},
		{"Tools & Diagnostics:", "gui"},
	}
	for _, tc := range cases {
		if !strings.Contains(section(tc.title), tc.command) {
			t.Errorf("expected %q under %q", tc.command, tc.title)
		}
	}
}

// TestRootHelpFallsBackForUngroupedCommand proves the Additional Commands
// branch of the template works, so TestRootHelpRendersGroupSections asserting
// its absence is meaningful rather than vacuous.
func TestRootHelpFallsBackForUngroupedCommand(t *testing.T) {
	root := newRootCommand()
	// Must be runnable: cobra's IsAvailableCommand (and so both the template
	// and AllChildCommandsHaveGroup) ignores commands with no Run.
	root.AddCommand(&cobra.Command{
		Use:   "ungrouped",
		Short: "no GroupID set",
		Run:   func(*cobra.Command, []string) {},
	})

	out := renderHelp(t, root)

	if !strings.Contains(out, "Additional Commands:") {
		t.Errorf("expected the Additional Commands fallback to render:\n%s", out)
	}
	if !strings.Contains(out, "ungrouped") {
		t.Errorf("expected the ungrouped command to be listed:\n%s", out)
	}
	if root.AllChildCommandsHaveGroup() {
		t.Error("AllChildCommandsHaveGroup() should be false with an ungrouped command")
	}
}

// TestEveryCommandHasGroupID guards the taxonomy: a command registered without
// a GroupID silently falls into the Additional Commands bucket.
func TestEveryCommandHasGroupID(t *testing.T) {
	root := newRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("executing root: %v", err)
	}

	for _, cmd := range root.Commands() {
		if cmd.GroupID == "" {
			t.Errorf("command %q has no GroupID", cmd.Name())
		}
	}
	if !root.AllChildCommandsHaveGroup() {
		t.Error("AllChildCommandsHaveGroup() is false")
	}
}

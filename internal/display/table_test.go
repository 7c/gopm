package display

import (
	"strings"
	"testing"
)

func TestVisibleLen(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{Bold("hi"), 2},
		{Red("err"), 3},
		{Dim("x") + Green("y"), 2},
		{"", 0},
	}
	for _, tt := range tests {
		got := visibleLen(tt.input)
		if got != tt.want {
			t.Errorf("visibleLen(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestStatusColor(t *testing.T) {
	// Just check that it returns non-empty strings with ANSI codes
	online := StatusColor("online")
	if !strings.Contains(online, "online") {
		t.Errorf("StatusColor(online) = %q, should contain 'online'", online)
	}
	if !strings.Contains(online, "\033[") {
		t.Error("StatusColor(online) should contain ANSI escape")
	}

	stopped := StatusColor("stopped")
	if !strings.Contains(stopped, "\033[33m") { // yellow
		t.Error("StatusColor(stopped) should be yellow")
	}

	errored := StatusColor("errored")
	if !strings.Contains(errored, "\033[31m") { // red
		t.Error("StatusColor(errored) should be red")
	}

	unknown := StatusColor("unknown")
	if strings.Contains(unknown, "\033[") {
		t.Error("StatusColor(unknown) should not have ANSI codes")
	}
}

func TestTableRender(t *testing.T) {
	tbl := NewTable("Name", "Value")
	tbl.AddRow("foo", "bar")
	tbl.AddRow("hello", "world")

	var buf strings.Builder
	tbl.Render(&buf)
	output := buf.String()

	// Should contain borders and data
	if !strings.Contains(output, "foo") {
		t.Error("table should contain 'foo'")
	}
	if !strings.Contains(output, "world") {
		t.Error("table should contain 'world'")
	}
	// Should have box-drawing characters
	if !strings.Contains(output, "┌") {
		t.Error("table should contain box-drawing characters")
	}
}

func TestColorHelpers(t *testing.T) {
	if Bold("x") == "x" {
		t.Error("Bold should add ANSI codes")
	}
	if Dim("x") == "x" {
		t.Error("Dim should add ANSI codes")
	}
	if Red("x") == "x" {
		t.Error("Red should add ANSI codes")
	}
	if Green("x") == "x" {
		t.Error("Green should add ANSI codes")
	}
	if Yellow("x") == "x" {
		t.Error("Yellow should add ANSI codes")
	}
	if Blue("x") == "x" {
		t.Error("Blue should add ANSI codes")
	}
	if Cyan("x") == "x" {
		t.Error("Cyan should add ANSI codes")
	}
	if Magenta("x") == "x" {
		t.Error("Magenta should add ANSI codes")
	}
}

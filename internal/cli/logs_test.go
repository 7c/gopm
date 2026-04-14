package cli

import (
	"strings"
	"testing"
)

func TestTimestampPrefix(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"normal iso", "2026-04-14T16:30:01.123Z hello world\n", "2026-04-14T16:30:01.123Z"},
		{"no timestamp", "hello world\n", ""},
		{"too short", "foo\n", ""},
		{"wrong format", "2026/04/14 16:30:01 hello\n", ""},
		{"tz offset", "2026-04-14T16:30:01-07:00 body\n", "2026-04-14T16:30:01-07:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := timestampPrefix(c.line)
			if got != c.want {
				t.Errorf("timestampPrefix(%q) = %q, want %q", c.line, got, c.want)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	in := "a\nb\nc\n"
	got := splitLines(in)
	if len(got) != 3 || got[0] != "a\n" || got[1] != "b\n" || got[2] != "c\n" {
		t.Errorf("splitLines trailing newline: %#v", got)
	}
	in2 := "a\nb"
	got2 := splitLines(in2)
	if len(got2) != 2 || got2[1] != "b\n" {
		t.Errorf("splitLines missing trailing newline should be appended: %#v", got2)
	}
	if splitLines("") != nil {
		t.Errorf("empty content should return nil")
	}
}

func TestMergeTaggedLines_ByTimestamp(t *testing.T) {
	out := []string{
		"2026-04-14T16:30:01.000Z stdout1\n",
		"2026-04-14T16:30:03.000Z stdout2\n",
	}
	errLines := []string{
		"2026-04-14T16:30:02.000Z stderr1\n",
		"2026-04-14T16:30:04.000Z stderr2\n",
	}
	merged := mergeTaggedLines(out, errLines)
	if len(merged) != 4 {
		t.Fatalf("len = %d, want 4", len(merged))
	}

	wantStreams := []logStream{streamStdout, streamStderr, streamStdout, streamStderr}
	wantContents := []string{"stdout1", "stderr1", "stdout2", "stderr2"}
	for i, tl := range merged {
		if tl.stream != wantStreams[i] {
			t.Errorf("merged[%d].stream = %d, want %d", i, tl.stream, wantStreams[i])
		}
		if !strings.Contains(tl.line, wantContents[i]) {
			t.Errorf("merged[%d].line = %q, want substring %q", i, tl.line, wantContents[i])
		}
	}
}

func TestMergeTaggedLines_EmptyStderr(t *testing.T) {
	out := []string{
		"2026-04-14T16:30:01.000Z only stdout\n",
	}
	merged := mergeTaggedLines(out, nil)
	if len(merged) != 1 || merged[0].stream != streamStdout {
		t.Errorf("want one stdout line, got %#v", merged)
	}
}

func TestMergeTaggedLines_EmptyStdout(t *testing.T) {
	errLines := []string{
		"2026-04-14T16:30:01.000Z only stderr\n",
	}
	merged := mergeTaggedLines(nil, errLines)
	if len(merged) != 1 || merged[0].stream != streamStderr {
		t.Errorf("want one stderr line, got %#v", merged)
	}
}

func TestFormatTaggedLine_HasStreamMarker(t *testing.T) {
	tl := taggedLine{
		stream: streamStderr,
		line:   "2026-04-14T16:30:01.000Z boom\n",
	}
	got := formatTaggedLine(tl, "")
	if !strings.Contains(got, "[ERR]") {
		t.Errorf("stderr format missing [ERR] marker: %q", got)
	}
	if !strings.Contains(got, "boom") {
		t.Errorf("stderr format missing body: %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("stderr format should preserve trailing newline: %q", got)
	}
}

func TestFormatTaggedLine_StdoutMarker(t *testing.T) {
	tl := taggedLine{
		stream: streamStdout,
		line:   "2026-04-14T16:30:01.000Z hi\n",
	}
	got := formatTaggedLine(tl, "")
	if !strings.Contains(got, "[OUT]") {
		t.Errorf("stdout format missing [OUT] marker: %q", got)
	}
}

func TestFormatTaggedLine_ProcPrefix(t *testing.T) {
	tl := taggedLine{
		stream: streamStdout,
		line:   "2026-04-14T16:30:01.000Z hi\n",
	}
	got := formatTaggedLine(tl, "PREFIX")
	if !strings.HasPrefix(got, "PREFIX") {
		t.Errorf("expected proc prefix at start: %q", got)
	}
}

func TestSplitByProcHeaders(t *testing.T) {
	content := "==> alpha <==\n" +
		"2026-04-14T16:30:01.000Z line1\n" +
		"2026-04-14T16:30:02.000Z line2\n" +
		"\n" +
		"==> beta <==\n" +
		"2026-04-14T16:30:03.000Z betaline\n"
	got := splitByProcHeaders(content)
	if len(got) != 2 {
		t.Fatalf("want 2 groups, got %d: %#v", len(got), got)
	}
	if !strings.Contains(got["alpha"], "line1") || !strings.Contains(got["alpha"], "line2") {
		t.Errorf("alpha body wrong: %q", got["alpha"])
	}
	if !strings.Contains(got["beta"], "betaline") {
		t.Errorf("beta body wrong: %q", got["beta"])
	}
	if strings.Contains(got["alpha"], "betaline") {
		t.Errorf("alpha body leaked into beta region")
	}
}

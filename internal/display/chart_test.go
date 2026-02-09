package display

import (
	"strings"
	"testing"
)

func TestBrailleCanvasSet(t *testing.T) {
	c := newBrailleCanvas(2, 2)
	// Set top-left dot (px=0, py=0): left col, row 0 → bit 0 → 0x01
	c.set(0, 0)
	lines := c.render()
	runes := []rune(lines[0])
	if runes[0] != 0x2801 {
		t.Errorf("expected U+2801, got U+%04X", runes[0])
	}
	// Second cell should be blank.
	if runes[1] != 0x2800 {
		t.Errorf("expected U+2800 (blank), got U+%04X", runes[1])
	}
}

func TestBrailleCanvasSetRightColumn(t *testing.T) {
	c := newBrailleCanvas(1, 1)
	// Set right column, row 0 (px=1, py=0): bit 3 → 0x08
	c.set(1, 0)
	runes := []rune(c.render()[0])
	if runes[0] != 0x2808 {
		t.Errorf("expected U+2808, got U+%04X", runes[0])
	}
}

func TestBrailleCanvasSetBottomRow(t *testing.T) {
	c := newBrailleCanvas(1, 1)
	// Left col, row 3 (px=0, py=3): bit 6 → 0x40
	c.set(0, 3)
	runes := []rune(c.render()[0])
	if runes[0] != 0x2840 {
		t.Errorf("expected U+2840, got U+%04X", runes[0])
	}

	// Right col, row 3 (px=1, py=3): bit 7 → 0x80
	c2 := newBrailleCanvas(1, 1)
	c2.set(1, 3)
	runes2 := []rune(c2.render()[0])
	if runes2[0] != 0x2880 {
		t.Errorf("expected U+2880, got U+%04X", runes2[0])
	}
}

func TestBrailleCanvasMultipleDots(t *testing.T) {
	c := newBrailleCanvas(1, 1)
	c.set(0, 0) // 0x01
	c.set(1, 0) // 0x08
	c.set(0, 3) // 0x40
	// Should be 0x01 | 0x08 | 0x40 = 0x49
	runes := []rune(c.render()[0])
	if runes[0] != 0x2849 {
		t.Errorf("expected U+2849, got U+%04X", runes[0])
	}
}

func TestBrailleCanvasOutOfBounds(t *testing.T) {
	c := newBrailleCanvas(2, 2)
	// These should not panic.
	c.set(-1, 0)
	c.set(0, -1)
	c.set(100, 0)
	c.set(0, 100)
}

func TestBrailleCanvasDrawLine(t *testing.T) {
	c := newBrailleCanvas(10, 5)
	c.drawLine(0, 0, 19, 19) // diagonal
	lines := c.render()
	nonEmpty := 0
	for _, line := range lines {
		for _, r := range line {
			if r != 0x2800 {
				nonEmpty++
			}
		}
	}
	if nonEmpty == 0 {
		t.Error("expected non-empty braille cells after drawLine")
	}
}

func TestBrailleCanvasDrawLineHorizontal(t *testing.T) {
	c := newBrailleCanvas(5, 1)
	c.drawLine(0, 2, 9, 2) // horizontal line across
	lines := c.render()
	// All cells in row 0 should have dots.
	for _, r := range []rune(lines[0]) {
		if r == 0x2800 {
			t.Error("expected all cells to have dots for horizontal line")
			break
		}
	}
}

func TestRenderChartSingleSeries(t *testing.T) {
	var buf strings.Builder
	series := []ChartSeries{{
		Name:  "test",
		Color: green,
		Points: []ChartPoint{
			{Time: 1000, Value: 10},
			{Time: 2000, Value: 50},
			{Time: 3000, Value: 30},
		},
	}}
	RenderChart(&buf, ChartConfig{
		Title:  "Test Chart",
		Width:  20,
		Height: 5,
	}, series)

	output := buf.String()
	if !strings.Contains(output, "Test Chart") {
		t.Error("output should contain chart title")
	}
	hasBraille := false
	for _, r := range output {
		if r >= 0x2800 && r <= 0x28FF {
			hasBraille = true
			break
		}
	}
	if !hasBraille {
		t.Error("output should contain braille characters")
	}
}

func TestRenderChartMultipleSeries(t *testing.T) {
	var buf strings.Builder
	series := []ChartSeries{
		{Name: "api", Points: []ChartPoint{{Time: 100, Value: 5}, {Time: 200, Value: 10}}},
		{Name: "worker", Points: []ChartPoint{{Time: 100, Value: 3}, {Time: 200, Value: 8}}},
	}
	AssignSeriesColors(series)
	RenderChart(&buf, ChartConfig{
		Title:  "Multi",
		Width:  20,
		Height: 5,
	}, series)

	output := buf.String()
	// Legend should contain both names.
	if !strings.Contains(output, "api") {
		t.Error("output should contain 'api' in legend")
	}
	if !strings.Contains(output, "worker") {
		t.Error("output should contain 'worker' in legend")
	}
}

func TestRenderChartEmpty(t *testing.T) {
	var buf strings.Builder
	RenderChart(&buf, ChartConfig{Title: "Empty"}, nil)
	if buf.Len() != 0 {
		t.Error("nil series should produce no output")
	}
}

func TestRenderChartNoPoints(t *testing.T) {
	var buf strings.Builder
	RenderChart(&buf, ChartConfig{Title: "NoData"}, []ChartSeries{
		{Name: "empty", Points: nil},
	})
	if buf.Len() != 0 {
		t.Error("series with no points should produce no output")
	}
}

func TestAssignSeriesColors(t *testing.T) {
	series := []ChartSeries{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}
	AssignSeriesColors(series)
	for i, s := range series {
		if s.Color == "" {
			t.Errorf("series[%d] should have a color assigned", i)
		}
	}
	// First three should get different colors.
	if series[0].Color == series[1].Color {
		t.Error("first two series should have different colors")
	}
}

func TestAssignSeriesColorsPreservesExisting(t *testing.T) {
	series := []ChartSeries{
		{Name: "a", Color: red},
		{Name: "b"},
	}
	AssignSeriesColors(series)
	if series[0].Color != red {
		t.Error("existing color should be preserved")
	}
	if series[1].Color == "" {
		t.Error("missing color should be assigned")
	}
}

func TestFormatCPUAxis(t *testing.T) {
	if got := FormatCPUAxis(12.3); got != "12.3%" {
		t.Errorf("FormatCPUAxis(12.3) = %q, want '12.3%%'", got)
	}
	if got := FormatCPUAxis(100.0); got != "100%" {
		t.Errorf("FormatCPUAxis(100) = %q, want '100%%'", got)
	}
}

func TestFormatRestartsAxis(t *testing.T) {
	if got := FormatRestartsAxis(5); got != "5" {
		t.Errorf("FormatRestartsAxis(5) = %q, want '5'", got)
	}
}

func TestFormatMemoryAxis(t *testing.T) {
	got := FormatMemoryAxis(1048576) // 1MB
	if !strings.Contains(got, "MB") {
		t.Errorf("FormatMemoryAxis(1MB) = %q, should contain 'MB'", got)
	}
}

func TestFormatUptimeAxis(t *testing.T) {
	got := FormatUptimeAxis(7200) // 2 hours
	if !strings.Contains(got, "h") {
		t.Errorf("FormatUptimeAxis(7200) = %q, should contain 'h'", got)
	}
	got = FormatUptimeAxis(86400) // 1 day
	if !strings.Contains(got, "d") {
		t.Errorf("FormatUptimeAxis(86400) = %q, should contain 'd'", got)
	}
}

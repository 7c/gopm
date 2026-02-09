package display

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// ChartSeries represents one data series to plot.
type ChartSeries struct {
	Name   string
	Color  string // ANSI color code (e.g., green, cyan)
	Points []ChartPoint
}

// ChartPoint is a single (timestamp, value) pair.
type ChartPoint struct {
	Time  int64   // Unix seconds
	Value float64
}

// ChartConfig configures the chart rendering.
type ChartConfig struct {
	Title      string
	Width      int                  // character columns for plot area (default 60)
	Height     int                  // character rows for plot area (default 15)
	YFormatter func(float64) string // custom Y-axis label formatter
}

// seriesColors is the palette for multi-series charts.
var seriesColors = []string{green, cyan, yellow, magenta, blue, red, white}

// AssignSeriesColors assigns colors from the palette to series without one.
func AssignSeriesColors(series []ChartSeries) {
	for i := range series {
		if series[i].Color == "" {
			series[i].Color = seriesColors[i%len(seriesColors)]
		}
	}
}

// brailleCanvas is a 2D grid of braille dot-pixels.
// Each character cell is 2 columns × 4 rows of sub-pixels.
// (0,0) is top-left. x ∈ [0, width*2), y ∈ [0, height*4).
type brailleCanvas struct {
	width  int       // in characters
	height int       // in characters
	dots   [][]uint8 // [height][width] accumulated braille bitmasks
}

func newBrailleCanvas(w, h int) *brailleCanvas {
	dots := make([][]uint8, h)
	for i := range dots {
		dots[i] = make([]uint8, w)
	}
	return &brailleCanvas{width: w, height: h, dots: dots}
}

// set activates a dot at sub-pixel coordinates (px, py).
func (c *brailleCanvas) set(px, py int) {
	if px < 0 || px >= c.width*2 || py < 0 || py >= c.height*4 {
		return
	}
	cx := px / 2
	cy := py / 4
	dx := px % 2 // 0=left, 1=right
	dy := py % 4 // 0=top, 3=bottom

	// Braille dot bit mapping:
	//   Left col (dx=0): rows 0-2 → bits 0-2 (0x01,0x02,0x04), row 3 → bit 6 (0x40)
	//   Right col (dx=1): rows 0-2 → bits 3-5 (0x08,0x10,0x20), row 3 → bit 7 (0x80)
	var bit uint8
	if dx == 0 {
		if dy < 3 {
			bit = 1 << uint(dy)
		} else {
			bit = 0x40
		}
	} else {
		if dy < 3 {
			bit = 1 << uint(dy+3)
		} else {
			bit = 0x80
		}
	}
	c.dots[cy][cx] |= bit
}

// drawLine draws a line between two sub-pixel coordinates using Bresenham's.
func (c *brailleCanvas) drawLine(x0, y0, x1, y1 int) {
	dx := iabs(x1 - x0)
	dy := iabs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy

	for {
		c.set(x0, y0)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// render returns the canvas as a slice of strings (one per row).
func (c *brailleCanvas) render() []string {
	lines := make([]string, c.height)
	for y := 0; y < c.height; y++ {
		var sb strings.Builder
		for x := 0; x < c.width; x++ {
			sb.WriteRune(rune(0x2800 + int(c.dots[y][x])))
		}
		lines[y] = sb.String()
	}
	return lines
}

func iabs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// RenderChart renders a braille line chart to the writer.
// Multiple series are overlaid with per-series colors.
func RenderChart(w io.Writer, cfg ChartConfig, series []ChartSeries) {
	if len(series) == 0 {
		return
	}

	width := cfg.Width
	if width <= 0 {
		width = 60
	}
	height := cfg.Height
	if height <= 0 {
		height = 15
	}

	// Compute global Y min/max and time range.
	var yMin, yMax float64
	var tMin, tMax int64
	first := true
	for _, s := range series {
		for _, p := range s.Points {
			if first {
				yMin, yMax = p.Value, p.Value
				tMin, tMax = p.Time, p.Time
				first = false
			} else {
				if p.Value < yMin {
					yMin = p.Value
				}
				if p.Value > yMax {
					yMax = p.Value
				}
				if p.Time < tMin {
					tMin = p.Time
				}
				if p.Time > tMax {
					tMax = p.Time
				}
			}
		}
	}
	if first {
		return // no data
	}

	// Pad Y range for aesthetics.
	yRange := yMax - yMin
	if yRange == 0 {
		yRange = 1
		yMax = yMin + 1
	}
	pad := yRange * 0.05
	yMin -= pad
	if yMin < 0 {
		yMin = 0
	}
	yMax += pad

	tRange := tMax - tMin
	if tRange == 0 {
		tRange = 1
	}

	pxWidth := width * 2
	pxHeight := height * 4

	// One canvas per series for per-series coloring.
	canvases := make([]*brailleCanvas, len(series))
	for si, s := range series {
		canvases[si] = newBrailleCanvas(width, height)
		if len(s.Points) == 0 {
			continue
		}
		prevPx, prevPy := -1, -1
		for _, p := range s.Points {
			px := int(float64(p.Time-tMin) / float64(tRange) * float64(pxWidth-1))
			py := int((1.0 - (p.Value-yMin)/(yMax-yMin)) * float64(pxHeight-1))
			if px < 0 {
				px = 0
			}
			if px >= pxWidth {
				px = pxWidth - 1
			}
			if py < 0 {
				py = 0
			}
			if py >= pxHeight {
				py = pxHeight - 1
			}
			if prevPx >= 0 {
				canvases[si].drawLine(prevPx, prevPy, px, py)
			} else {
				canvases[si].set(px, py)
			}
			prevPx, prevPy = px, py
		}
	}

	// Y-axis formatter.
	yFmt := cfg.YFormatter
	if yFmt == nil {
		yFmt = func(v float64) string { return fmt.Sprintf("%.1f", v) }
	}
	yLabelWidth := 8

	// Title.
	if cfg.Title != "" {
		fmt.Fprintf(w, "  %s\n", Bold(cfg.Title))
	}

	// Composite output: per-cell, pick color from the series that drew dots.
	for row := 0; row < height; row++ {
		// Y-axis label.
		label := ""
		switch {
		case row == 0:
			label = yFmt(yMax)
		case row == height/4:
			label = yFmt(yMin + (yMax-yMin)*0.75)
		case row == height/2:
			label = yFmt(yMin + (yMax-yMin)*0.5)
		case row == height*3/4:
			label = yFmt(yMin + (yMax-yMin)*0.25)
		case row == height-1:
			label = yFmt(yMin)
		}
		fmt.Fprintf(w, "  %*s %s", yLabelWidth, Dim(label), dim+"│"+reset)

		// Render each cell with appropriate color.
		for col := 0; col < width; col++ {
			chosen := -1
			var merged uint8
			for si, cv := range canvases {
				if cv.dots[row][col] != 0 {
					merged |= cv.dots[row][col]
					if chosen < 0 {
						chosen = si
					}
				}
			}
			if merged == 0 {
				fmt.Fprint(w, string(rune(0x2800))) // blank braille
			} else {
				color := series[chosen].Color
				if color == "" {
					color = cyan
				}
				fmt.Fprintf(w, "%s%s%s", color, string(rune(0x2800+int(merged))), reset)
			}
		}
		fmt.Fprintln(w)
	}

	// X-axis line.
	fmt.Fprintf(w, "  %*s %s", yLabelWidth, "", dim+"└"+strings.Repeat("─", width)+reset)
	fmt.Fprintln(w)

	// X-axis time labels.
	printTimeAxis(w, tMin, tMax, width, yLabelWidth)

	// Legend when multiple series.
	if len(series) > 1 {
		fmt.Fprintf(w, "  %*s  ", yLabelWidth, "")
		for i, s := range series {
			if i > 0 {
				fmt.Fprint(w, "  ")
			}
			color := s.Color
			if color == "" {
				color = cyan
			}
			fmt.Fprintf(w, "%s━━%s %s", color, reset, s.Name)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w)
}

func printTimeAxis(w io.Writer, tMin, tMax int64, width, yLabelWidth int) {
	numLabels := 5
	if width < 30 {
		numLabels = 3
	}

	labels := make([]string, numLabels)
	positions := make([]int, numLabels)
	for i := 0; i < numLabels; i++ {
		ts := tMin + int64(i)*((tMax-tMin)/int64(numLabels-1))
		labels[i] = time.Unix(ts, 0).Format("15:04")
		positions[i] = i * width / (numLabels - 1)
	}

	// Build the axis line character by character.
	fmt.Fprintf(w, "  %*s  ", yLabelWidth, "")
	pos := 0
	for i := 0; i < numLabels; i++ {
		gap := positions[i] - pos
		if gap < 0 {
			gap = 0
		}
		fmt.Fprint(w, strings.Repeat(" ", gap))
		fmt.Fprint(w, Dim(labels[i]))
		pos = positions[i] + len(labels[i])
	}
	fmt.Fprintln(w)
}

// Predefined Y-axis formatters.

// FormatCPUAxis formats a CPU percentage value for the Y axis.
func FormatCPUAxis(v float64) string {
	if v >= 100 {
		return fmt.Sprintf("%.0f%%", v)
	}
	return fmt.Sprintf("%.1f%%", v)
}

// FormatMemoryAxis formats a byte count for the Y axis.
func FormatMemoryAxis(v float64) string {
	return protocol.FormatBytes(uint64(v))
}

// FormatUptimeAxis formats seconds as a human-readable duration for the Y axis.
func FormatUptimeAxis(v float64) string {
	d := time.Duration(int64(v)) * time.Second
	if d >= 24*time.Hour {
		return fmt.Sprintf("%.1fd", d.Hours()/24)
	}
	if d >= time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.0fm", d.Minutes())
}

// FormatRestartsAxis formats a restart count for the Y axis.
func FormatRestartsAxis(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

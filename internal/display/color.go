package display

import "fmt"

// ANSI color codes for terminal output.
// Using raw ANSI to avoid pulling lipgloss into every CLI command.
const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	dim     = "\033[2m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	white   = "\033[37m"
)

// Exported constants for use in help templates.
const (
	CReset   = reset
	CBold    = bold
	CDim     = dim
	CRed     = red
	CGreen   = green
	CYellow  = yellow
	CBlue    = blue
	CMagenta = magenta
	CCyan    = cyan
	CWhite   = white
)

// Color helpers for CLI output. Each returns the styled string.

func Bold(s string) string    { return bold + s + reset }
func Dim(s string) string     { return dim + s + reset }
func Red(s string) string     { return red + s + reset }
func Green(s string) string   { return green + s + reset }
func Yellow(s string) string  { return yellow + s + reset }
func Blue(s string) string    { return blue + s + reset }
func Magenta(s string) string { return magenta + s + reset }
func Cyan(s string) string    { return cyan + s + reset }

// StatusColor returns the status string colored by state.
func StatusColor(status string) string {
	switch status {
	case "online":
		return green + status + reset
	case "stopped":
		return yellow + status + reset
	case "errored":
		return red + status + reset
	default:
		return status
	}
}

// colorLen returns the number of non-ANSI visible characters.
// Used for padding calculations when strings contain color codes.
func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		if r == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		n++
	}
	return n
}

// padRight pads a string to width based on visible length (ignoring ANSI codes).
func padRight(s string, width int) string {
	vis := visibleLen(s)
	if vis >= width {
		return s
	}
	return s + fmt.Sprintf("%*s", width-vis, "")
}

package gui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("63"))

	statusOnline = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	statusStopped = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	statusErrored = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color("236"))

	helpStyle = lipgloss.NewStyle().
			Faint(true)

	logStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1)
)

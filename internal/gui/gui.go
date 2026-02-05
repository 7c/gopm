package gui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Pane identifies the active pane in the TUI.
type Pane int

const (
	ProcessList Pane = iota
	LogViewer
)

// model is the Bubble Tea model for the GoPM dashboard.
type model struct {
	client      *client.Client
	processes   []protocol.ProcessInfo
	selected    int
	logLines    []string
	activePane  Pane
	refreshRate time.Duration

	daemonPID     int
	daemonUptime  string
	daemonVersion string

	width  int
	height int

	showDetail bool
	detailProc protocol.ProcessInfo

	logMode string // "stdout" or "stderr"

	statusMsg    string
	statusExpiry time.Time
}

// tickMsg fires on every refresh interval.
type tickMsg time.Time

// statusClearMsg clears the status message.
type statusClearMsg struct{}

// Run starts the Bubble Tea TUI program.
func Run(c *client.Client, refreshRate time.Duration) error {
	m := model{
		client:      c,
		refreshRate: refreshRate,
		logMode:     "stdout",
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tickCmd(m.refreshRate), tea.EnterAltScreen)
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		return m.handleTick()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case statusClearMsg:
		m.statusMsg = ""
		return m, nil
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global keys.
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	}

	// Detail overlay keys.
	if m.showDetail {
		switch key {
		case "esc", "enter":
			m.showDetail = false
		}
		return m, nil
	}

	switch key {
	case "up", "k":
		if m.activePane == ProcessList && m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.activePane == ProcessList && m.selected < len(m.processes)-1 {
			m.selected++
		}

	case "tab":
		if m.activePane == ProcessList {
			m.activePane = LogViewer
		} else {
			m.activePane = ProcessList
		}

	case "enter":
		if m.activePane == ProcessList && len(m.processes) > 0 {
			m.showDetail = true
			m.detailProc = m.processes[m.selected]
		}

	case "t":
		return m.sendAction("stop")
	case "r":
		return m.sendAction("restart")
	case "d":
		return m.sendAction("delete")
	case "f":
		return m.sendAction("flush")

	case "l":
		if m.activePane == ProcessList {
			m.activePane = LogViewer
		} else {
			m.activePane = ProcessList
		}

	case "e":
		if m.logMode == "stdout" {
			m.logMode = "stderr"
		} else {
			m.logMode = "stdout"
		}

	case "s":
		m.statusMsg = "Use CLI to start a process: gopm start <command>"
		m.statusExpiry = time.Now().Add(3 * time.Second)
		return m, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
			return statusClearMsg{}
		})
	}

	return m, nil
}

func (m model) sendAction(method string) (tea.Model, tea.Cmd) {
	if len(m.processes) == 0 {
		return m, nil
	}
	proc := m.processes[m.selected]
	target := protocol.TargetParams{Target: proc.Name}
	resp, err := m.client.Send(method, target)
	if err != nil {
		m.statusMsg = fmt.Sprintf("Error: %s", err)
	} else if !resp.Success {
		m.statusMsg = fmt.Sprintf("Error: %s", resp.Error)
	} else {
		m.statusMsg = fmt.Sprintf("%s: %s OK", method, proc.Name)
	}
	m.statusExpiry = time.Now().Add(3 * time.Second)
	return m, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return statusClearMsg{}
	})
}

func (m model) handleTick() (tea.Model, tea.Cmd) {
	// Refresh process list.
	if resp, err := m.client.Send("list", nil); err == nil && resp.Success {
		var procs []protocol.ProcessInfo
		if json.Unmarshal(resp.Data, &procs) == nil {
			m.processes = procs
			// Clamp selection.
			if m.selected >= len(m.processes) {
				m.selected = max(0, len(m.processes)-1)
			}
		}
	}

	// Refresh daemon info.
	if resp, err := m.client.Send("ping", nil); err == nil && resp.Success {
		var ping protocol.PingResult
		if json.Unmarshal(resp.Data, &ping) == nil {
			m.daemonPID = ping.PID
			m.daemonUptime = ping.Uptime
			m.daemonVersion = ping.Version
		}
	}

	// Fetch logs for selected process.
	if len(m.processes) > 0 && m.selected < len(m.processes) {
		proc := m.processes[m.selected]
		params := protocol.LogsParams{
			Target:  proc.Name,
			Lines:   50,
			ErrOnly: m.logMode == "stderr",
		}
		if resp, err := m.client.Send("logs", params); err == nil && resp.Success {
			var result struct {
				Content string `json:"content"`
			}
			if json.Unmarshal(resp.Data, &result) == nil {
				lines := strings.Split(result.Content, "\n")
				m.logLines = lines
			}
		}
	} else {
		m.logLines = nil
	}

	// Clear expired status message.
	if m.statusMsg != "" && time.Now().After(m.statusExpiry) {
		m.statusMsg = ""
	}

	return m, tickCmd(m.refreshRate)
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Header.
	version := m.daemonVersion
	if version == "" {
		version = "?"
	}
	header := titleStyle.Render(fmt.Sprintf(
		"GoPM v%s \u2014 daemon PID: %d \u2014 uptime: %s",
		version, m.daemonPID, m.daemonUptime,
	))
	b.WriteString(header)
	b.WriteString("\n\n")

	// Process table.
	tableStr := m.renderTable()
	b.WriteString(tableStr)
	b.WriteString("\n")

	// Status message.
	if m.statusMsg != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(m.statusMsg))
		b.WriteString("\n")
	}

	// Log viewer.
	logTitle := fmt.Sprintf(" Logs (%s) ", m.logMode)
	if len(m.processes) > 0 && m.selected < len(m.processes) {
		logTitle = fmt.Sprintf(" Logs [%s] (%s) ", m.processes[m.selected].Name, m.logMode)
	}
	paneIndicator := ""
	if m.activePane == LogViewer {
		paneIndicator = " *"
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")).Render(logTitle+paneIndicator) + "\n")

	logHeight := m.logViewHeight()
	logContent := m.renderLogs(logHeight)
	logWidth := max(m.width-4, 20)
	styledLog := logStyle.Width(logWidth).Height(logHeight).Render(logContent)
	b.WriteString(styledLog)
	b.WriteString("\n")

	// Help bar.
	help := helpStyle.Render(
		"[s]tart  s[t]op  [r]estart  [d]elete  [f]lush  [l]ogs  [e]rr/out  [\u2191\u2193] nav  [tab] pane  [enter] detail  [q] quit",
	)
	b.WriteString(help)

	// Detail overlay.
	if m.showDetail {
		overlay := m.renderDetail()
		return m.overlayCenter(b.String(), overlay)
	}

	return b.String()
}

// renderTable renders the process list as a simple aligned table.
func (m model) renderTable() string {
	headers := []string{"ID", "Name", "Status", "PID", "CPU", "Memory", "Restarts", "Uptime"}

	// Compute column widths.
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}

	type rowData struct {
		cols   []string
		status protocol.Status
	}

	var rows []rowData
	for _, p := range m.processes {
		pid := "-"
		cpu := "-"
		mem := "-"
		uptime := "-"
		if p.Status == protocol.StatusOnline && p.PID > 0 {
			pid = fmt.Sprintf("%d", p.PID)
			cpu = fmt.Sprintf("%.1f%%", p.CPU)
			mem = protocol.FormatBytes(p.Memory)
			if !p.Uptime.IsZero() {
				uptime = protocol.FormatDuration(time.Since(p.Uptime))
			}
		}
		cols := []string{
			fmt.Sprintf("%d", p.ID),
			p.Name,
			string(p.Status),
			pid,
			cpu,
			mem,
			fmt.Sprintf("%d", p.Restarts),
			uptime,
		}
		for i, c := range cols {
			if len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
		rows = append(rows, rowData{cols: cols, status: p.Status})
	}

	// Enforce a minimum total width, distribute if needed.
	fmtRow := func(cols []string, style lipgloss.Style) string {
		var parts []string
		for i, c := range cols {
			parts = append(parts, fmt.Sprintf("%-*s", widths[i], c))
		}
		return style.Render(strings.Join(parts, "  "))
	}

	paneIndicator := ""
	if m.activePane == ProcessList {
		paneIndicator = " *"
	}

	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")).Render(" Processes"+paneIndicator) + "\n")

	// Header row.
	headerRow := fmtRow(headers, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")))
	sb.WriteString(" " + headerRow + "\n")

	// Separator.
	totalWidth := 0
	for _, w := range widths {
		totalWidth += w
	}
	totalWidth += (len(widths) - 1) * 2 // account for column gaps
	sb.WriteString(" " + lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("\u2500", totalWidth)) + "\n")

	// Data rows.
	for i, row := range rows {
		// Color the status cell.
		styledCols := make([]string, len(row.cols))
		copy(styledCols, row.cols)
		styledCols[2] = colorStatus(row.status, styledCols[2])

		var rowStyle lipgloss.Style
		if i == m.selected {
			rowStyle = selectedStyle
		} else {
			rowStyle = lipgloss.NewStyle()
		}

		// Build row manually so the status color is preserved.
		var parts []string
		for ci, c := range styledCols {
			if ci == 2 {
				// Status column: pad the visible width, not the ANSI-laden string.
				padding := widths[ci] - len(row.cols[ci])
				if padding < 0 {
					padding = 0
				}
				parts = append(parts, c+strings.Repeat(" ", padding))
			} else {
				parts = append(parts, fmt.Sprintf("%-*s", widths[ci], c))
			}
		}
		line := strings.Join(parts, "  ")
		if i == m.selected {
			line = rowStyle.Render(line)
		}
		sb.WriteString(" " + line + "\n")
	}

	if len(rows) == 0 {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("  No processes running") + "\n")
	}

	return sb.String()
}

// colorStatus applies the appropriate color to a status string.
func colorStatus(s protocol.Status, text string) string {
	switch s {
	case protocol.StatusOnline:
		return statusOnline.Render(text)
	case protocol.StatusStopped:
		return statusStopped.Render(text)
	case protocol.StatusErrored:
		return statusErrored.Render(text)
	default:
		return text
	}
}

// logViewHeight computes how many lines to allocate for the log viewer.
func (m model) logViewHeight() int {
	// Reserve lines for: header(2) + table header(2) + separator(1) + rows + blank(1) + status(1) + logTitle(1) + help(1) + border(2)
	overhead := 11
	if m.statusMsg != "" {
		overhead++
	}
	rows := len(m.processes)
	if rows == 0 {
		rows = 1
	}
	h := m.height - overhead - rows
	if h < 3 {
		h = 3
	}
	return h
}

// renderLogs renders log lines trimmed to fit the available height.
func (m model) renderLogs(height int) string {
	if len(m.logLines) == 0 {
		return lipgloss.NewStyle().Faint(true).Render("No log data")
	}

	lines := m.logLines
	// Show only the tail that fits.
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}

	// Truncate long lines to fit width.
	maxWidth := m.width - 6
	if maxWidth < 20 {
		maxWidth = 20
	}
	var trimmed []string
	for _, l := range lines {
		if len(l) > maxWidth {
			l = l[:maxWidth-1] + "\u2026"
		}
		trimmed = append(trimmed, l)
	}

	return strings.Join(trimmed, "\n")
}

// renderDetail renders the process detail overlay.
func (m model) renderDetail() string {
	p := m.detailProc
	var sb strings.Builder

	kvLine := func(key, val string) {
		sb.WriteString(fmt.Sprintf("  %-16s %s\n", key+":", val))
	}

	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")).Render("  Process Detail") + "\n")
	sb.WriteString(strings.Repeat("\u2500", 44) + "\n")
	kvLine("Name", p.Name)
	kvLine("ID", fmt.Sprintf("%d", p.ID))
	kvLine("Status", string(p.Status))
	if p.Status == protocol.StatusOnline && p.PID > 0 {
		kvLine("PID", fmt.Sprintf("%d", p.PID))
	} else {
		kvLine("PID", "-")
	}
	kvLine("Command", p.Command)
	if len(p.Args) > 0 {
		kvLine("Args", strings.Join(p.Args, " "))
	}
	kvLine("CWD", p.Cwd)
	if p.Interpreter != "" {
		kvLine("Interpreter", p.Interpreter)
	}
	if p.Status == protocol.StatusOnline && !p.Uptime.IsZero() {
		kvLine("Uptime", protocol.FormatDuration(time.Since(p.Uptime)))
	} else {
		kvLine("Uptime", "-")
	}
	kvLine("Created", p.CreatedAt.Format("2006-01-02 15:04:05"))
	kvLine("Restarts", fmt.Sprintf("%d", p.Restarts))
	if p.Status != protocol.StatusOnline {
		kvLine("Exit Code", fmt.Sprintf("%d", p.ExitCode))
	}
	if p.Status == protocol.StatusOnline && p.PID > 0 {
		kvLine("CPU", fmt.Sprintf("%.1f%%", p.CPU))
		kvLine("Memory", protocol.FormatBytes(p.Memory))
	}
	kvLine("Auto Restart", string(p.RestartPolicy.AutoRestart))
	kvLine("Max Restarts", fmt.Sprintf("%d", p.RestartPolicy.MaxRestarts))
	kvLine("Stdout Log", p.LogOut)
	kvLine("Stderr Log", p.LogErr)

	sb.WriteString(strings.Repeat("\u2500", 44) + "\n")
	sb.WriteString(helpStyle.Render("  Press [esc] or [enter] to close"))

	return sb.String()
}

// overlayCenter places an overlay panel in the center of the base view.
func (m model) overlayCenter(base, overlay string) string {
	overlayLines := strings.Split(overlay, "\n")
	overlayWidth := 0
	for _, l := range overlayLines {
		if lipgloss.Width(l) > overlayWidth {
			overlayWidth = lipgloss.Width(l)
		}
	}
	overlayWidth += 4 // padding

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(overlayWidth)

	box := boxStyle.Render(overlay)
	boxLines := strings.Split(box, "\n")
	boxHeight := len(boxLines)
	boxWidth := lipgloss.Width(box)

	baseLines := strings.Split(base, "\n")

	// Vertical centering.
	startY := (m.height - boxHeight) / 2
	if startY < 0 {
		startY = 0
	}
	// Horizontal centering.
	startX := (m.width - boxWidth) / 2
	if startX < 0 {
		startX = 0
	}

	// Extend base lines if needed.
	for len(baseLines) < startY+boxHeight {
		baseLines = append(baseLines, "")
	}

	for i, boxLine := range boxLines {
		y := startY + i
		if y >= len(baseLines) {
			break
		}
		baseLine := baseLines[y]
		baseVisWidth := lipgloss.Width(baseLine)

		// Build replacement line: [left of base] [box line] [right of base]
		var result strings.Builder
		if startX > 0 {
			if baseVisWidth >= startX {
				// Truncate base to startX visible characters.
				result.WriteString(truncateVisual(baseLine, startX))
			} else {
				result.WriteString(baseLine)
				result.WriteString(strings.Repeat(" ", startX-baseVisWidth))
			}
		}
		result.WriteString(boxLine)

		baseLines[y] = result.String()
	}

	// Trim to terminal height.
	if len(baseLines) > m.height {
		baseLines = baseLines[:m.height]
	}

	return strings.Join(baseLines, "\n")
}

// truncateVisual truncates a string to n visible characters (best effort).
func truncateVisual(s string, n int) string {
	if n <= 0 {
		return ""
	}
	// Simple rune-based truncation; ignores ANSI widths for simplicity.
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

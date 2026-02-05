package display

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// Table renders bordered tables for CLI output.
type Table struct {
	headers []string
	rows    [][]string
	widths  []int
}

// NewTable creates a new table with the given headers.
func NewTable(headers ...string) *Table {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	return &Table{headers: headers, widths: widths}
}

// AddRow adds a row to the table.
func (t *Table) AddRow(cols ...string) {
	for i, c := range cols {
		if i < len(t.widths) && len(c) > t.widths[i] {
			t.widths[i] = len(c)
		}
	}
	t.rows = append(t.rows, cols)
}

// Render writes the table to the given writer.
func (t *Table) Render(w io.Writer) {
	if len(t.rows) == 0 && len(t.headers) == 0 {
		return
	}
	t.line(w, "┌", "┬", "┐")
	t.row(w, t.headers)
	t.line(w, "├", "┼", "┤")
	for _, r := range t.rows {
		t.row(w, r)
	}
	t.line(w, "└", "┴", "┘")
}

func (t *Table) line(w io.Writer, left, mid, right string) {
	fmt.Fprint(w, left)
	for i, width := range t.widths {
		fmt.Fprint(w, strings.Repeat("─", width+2))
		if i < len(t.widths)-1 {
			fmt.Fprint(w, mid)
		}
	}
	fmt.Fprintln(w, right)
}

func (t *Table) row(w io.Writer, cols []string) {
	fmt.Fprint(w, "│")
	for i, width := range t.widths {
		val := ""
		if i < len(cols) {
			val = cols[i]
		}
		fmt.Fprintf(w, " %-*s │", width, val)
	}
	fmt.Fprintln(w)
}

// RenderProcessList renders the process list table.
func RenderProcessList(w io.Writer, procs []protocol.ProcessInfo) {
	tbl := NewTable("ID", "Name", "Status", "PID", "CPU", "Memory", "Restart", "Uptime")
	for _, p := range procs {
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
		tbl.AddRow(
			fmt.Sprintf("%d", p.ID),
			p.Name,
			string(p.Status),
			pid,
			cpu,
			mem,
			fmt.Sprintf("%d", p.Restarts),
			uptime,
		)
	}
	tbl.Render(w)
}

// RenderDescribe renders the describe output as a key-value table.
func RenderDescribe(w io.Writer, p protocol.ProcessInfo) {
	tbl := NewTable("Key", "Value")
	tbl.AddRow("Name", p.Name)
	tbl.AddRow("ID", fmt.Sprintf("%d", p.ID))
	tbl.AddRow("Status", string(p.Status))
	if p.Status == protocol.StatusOnline && p.PID > 0 {
		tbl.AddRow("PID", fmt.Sprintf("%d", p.PID))
	} else {
		tbl.AddRow("PID", "-")
	}
	tbl.AddRow("Command", p.Command)
	if len(p.Args) > 0 {
		tbl.AddRow("Args", strings.Join(p.Args, " "))
	} else {
		tbl.AddRow("Args", "-")
	}
	tbl.AddRow("CWD", p.Cwd)
	if p.Interpreter != "" {
		tbl.AddRow("Interpreter", p.Interpreter)
	} else {
		tbl.AddRow("Interpreter", "-")
	}
	if p.Status == protocol.StatusOnline && !p.Uptime.IsZero() {
		tbl.AddRow("Uptime", protocol.FormatDuration(time.Since(p.Uptime)))
	} else {
		tbl.AddRow("Uptime", "-")
	}
	tbl.AddRow("Created At", p.CreatedAt.Format("2006-01-02 15:04:05 MST"))
	tbl.AddRow("Restarts", fmt.Sprintf("%d", p.Restarts))
	if p.Status != protocol.StatusOnline {
		tbl.AddRow("Last Exit Code", fmt.Sprintf("%d", p.ExitCode))
	} else {
		tbl.AddRow("Last Exit Code", "-")
	}
	if p.Status == protocol.StatusOnline && p.PID > 0 {
		tbl.AddRow("CPU", fmt.Sprintf("%.1f%%", p.CPU))
		tbl.AddRow("Memory", protocol.FormatBytes(p.Memory))
	} else {
		tbl.AddRow("CPU", "-")
		tbl.AddRow("Memory", "-")
	}
	tbl.AddRow("Auto Restart", string(p.RestartPolicy.AutoRestart))
	tbl.AddRow("Max Restarts", fmt.Sprintf("%d", p.RestartPolicy.MaxRestarts))
	tbl.AddRow("Min Uptime", p.RestartPolicy.MinUptime.String())
	tbl.AddRow("Restart Delay", p.RestartPolicy.RestartDelay.String())
	tbl.AddRow("Exp Backoff", fmt.Sprintf("%v", p.RestartPolicy.ExpBackoff))
	tbl.AddRow("Kill Signal", fmt.Sprintf("signal %d", p.RestartPolicy.KillSignal))
	tbl.AddRow("Kill Timeout", p.RestartPolicy.KillTimeout.String())
	tbl.AddRow("Stdout Log", p.LogOut)
	tbl.AddRow("Stderr Log", p.LogErr)
	if len(p.Env) > 0 {
		first := true
		for k, v := range p.Env {
			if first {
				tbl.AddRow("Env", fmt.Sprintf("%s=%s", k, v))
				first = false
			} else {
				tbl.AddRow("", fmt.Sprintf("%s=%s", k, v))
			}
		}
	} else {
		tbl.AddRow("Env", "-")
	}
	tbl.Render(w)
}

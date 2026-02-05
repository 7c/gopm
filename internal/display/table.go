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
	rows    [][]string // raw values (no color) for width calculation
	colored [][]string // colored values for rendering
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

// AddRow adds a row to the table. raw values are used for width; colored for display.
func (t *Table) AddRow(cols ...string) {
	for i, c := range cols {
		if i < len(t.widths) && len(c) > t.widths[i] {
			t.widths[i] = len(c)
		}
	}
	t.rows = append(t.rows, cols)
	t.colored = append(t.colored, cols) // default: same as raw
}

// AddColoredRow adds a row with separate raw (for widths) and colored (for display) values.
func (t *Table) AddColoredRow(raw []string, colored []string) {
	for i, c := range raw {
		if i < len(t.widths) && len(c) > t.widths[i] {
			t.widths[i] = len(c)
		}
	}
	t.rows = append(t.rows, raw)
	t.colored = append(t.colored, colored)
}

// Render writes the table to the given writer with dim borders and bold headers.
func (t *Table) Render(w io.Writer) {
	if len(t.rows) == 0 && len(t.headers) == 0 {
		return
	}
	t.line(w, "┌", "┬", "┐")
	t.headerRow(w)
	t.line(w, "├", "┼", "┤")
	for i := range t.rows {
		t.coloredRow(w, t.rows[i], t.colored[i])
	}
	t.line(w, "└", "┴", "┘")
}

func (t *Table) line(w io.Writer, left, mid, right string) {
	fmt.Fprint(w, dim+left)
	for i, width := range t.widths {
		fmt.Fprint(w, strings.Repeat("─", width+2))
		if i < len(t.widths)-1 {
			fmt.Fprint(w, mid)
		}
	}
	fmt.Fprintln(w, right+reset)
}

func (t *Table) headerRow(w io.Writer) {
	fmt.Fprint(w, dim+"│"+reset)
	for i, width := range t.widths {
		h := ""
		if i < len(t.headers) {
			h = t.headers[i]
		}
		fmt.Fprintf(w, " "+bold+"%-*s"+reset+" "+dim+"│"+reset, width, h)
	}
	fmt.Fprintln(w)
}

func (t *Table) coloredRow(w io.Writer, rawCols, colorCols []string) {
	fmt.Fprint(w, dim+"│"+reset)
	for i, width := range t.widths {
		raw := ""
		col := ""
		if i < len(rawCols) {
			raw = rawCols[i]
		}
		if i < len(colorCols) {
			col = colorCols[i]
		}
		// Pad based on raw (visible) length
		padding := width - len(raw)
		if padding < 0 {
			padding = 0
		}
		fmt.Fprintf(w, " %s%*s "+dim+"│"+reset, col, padding, "")
	}
	fmt.Fprintln(w)
}

// RenderProcessList renders the process list table with colored status.
func RenderProcessList(w io.Writer, procs []protocol.ProcessInfo) {
	tbl := NewTable("ID", "Name", "Status", "PID", "CPU", "Memory", "Restart", "Uptime")
	for _, p := range procs {
		pid := Dim("-")
		cpu := Dim("-")
		mem := Dim("-")
		uptime := Dim("-")
		rawPid := "-"
		rawCpu := "-"
		rawMem := "-"
		rawUptime := "-"
		if p.Status == protocol.StatusOnline && p.PID > 0 {
			rawPid = fmt.Sprintf("%d", p.PID)
			rawCpu = fmt.Sprintf("%.1f%%", p.CPU)
			rawMem = protocol.FormatBytes(p.Memory)
			pid = rawPid
			cpu = rawCpu
			mem = rawMem
			if !p.Uptime.IsZero() {
				rawUptime = protocol.FormatDuration(time.Since(p.Uptime))
				uptime = rawUptime
			}
		}
		rawStatus := string(p.Status)
		colorStatus := StatusColor(rawStatus)
		if p.StatusReason != "" && p.Status == protocol.StatusErrored {
			rawStatus += " (" + p.StatusReason + ")"
			colorStatus += Dim(" ("+p.StatusReason+")")
		}

		raw := []string{
			fmt.Sprintf("%d", p.ID),
			p.Name,
			rawStatus,
			rawPid,
			rawCpu,
			rawMem,
			fmt.Sprintf("%d", p.Restarts),
			rawUptime,
		}
		colored := []string{
			Dim(fmt.Sprintf("%d", p.ID)),
			Bold(p.Name),
			colorStatus,
			pid,
			cpu,
			mem,
			fmt.Sprintf("%d", p.Restarts),
			uptime,
		}
		tbl.AddColoredRow(raw, colored)
	}
	tbl.Render(w)
}

// RenderDescribe renders the describe output as a key-value table with colored status.
func RenderDescribe(w io.Writer, p protocol.ProcessInfo) {
	tbl := NewTable("Key", "Value")
	addKV := func(k, v string) {
		tbl.AddColoredRow([]string{k, v}, []string{Cyan(k), v})
	}
	addKVc := func(k, rawV, colorV string) {
		tbl.AddColoredRow([]string{k, rawV}, []string{Cyan(k), colorV})
	}

	addKVc("Name", p.Name, Bold(p.Name))
	addKV("ID", fmt.Sprintf("%d", p.ID))
	addKVc("Status", string(p.Status), StatusColor(string(p.Status)))
	if p.StatusReason != "" {
		addKVc("Status Reason", p.StatusReason, Yellow(p.StatusReason))
	}
	if p.Status == protocol.StatusOnline && p.PID > 0 {
		addKV("PID", fmt.Sprintf("%d", p.PID))
	} else {
		addKVc("PID", "-", Dim("-"))
	}
	addKV("Command", p.Command)
	if len(p.Args) > 0 {
		addKV("Args", strings.Join(p.Args, " "))
	} else {
		addKVc("Args", "-", Dim("-"))
	}
	addKV("CWD", p.Cwd)
	if p.Interpreter != "" {
		addKV("Interpreter", p.Interpreter)
	} else {
		addKVc("Interpreter", "-", Dim("-"))
	}
	if p.Status == protocol.StatusOnline && !p.Uptime.IsZero() {
		addKV("Uptime", protocol.FormatDuration(time.Since(p.Uptime)))
	} else {
		addKVc("Uptime", "-", Dim("-"))
	}
	addKV("Created At", p.CreatedAt.Format("2006-01-02 15:04:05 MST"))
	addKV("Restarts", fmt.Sprintf("%d", p.Restarts))
	if p.Status != protocol.StatusOnline {
		addKV("Last Exit Code", fmt.Sprintf("%d", p.ExitCode))
	} else {
		addKVc("Last Exit Code", "-", Dim("-"))
	}
	if p.Status == protocol.StatusOnline && p.PID > 0 {
		addKV("CPU", fmt.Sprintf("%.1f%%", p.CPU))
		addKV("Memory", protocol.FormatBytes(p.Memory))
	} else {
		addKVc("CPU", "-", Dim("-"))
		addKVc("Memory", "-", Dim("-"))
	}
	addKV("Auto Restart", string(p.RestartPolicy.AutoRestart))
	addKV("Max Restarts", fmt.Sprintf("%d", p.RestartPolicy.MaxRestarts))
	addKV("Min Uptime", p.RestartPolicy.MinUptime.String())
	addKV("Restart Delay", p.RestartPolicy.RestartDelay.String())
	addKV("Exp Backoff", fmt.Sprintf("%v", p.RestartPolicy.ExpBackoff))
	addKV("Kill Signal", fmt.Sprintf("signal %d", p.RestartPolicy.KillSignal))
	addKV("Kill Timeout", p.RestartPolicy.KillTimeout.String())
	addKV("Stdout Log", p.LogOut)
	addKV("Stderr Log", p.LogErr)
	if len(p.Env) > 0 {
		first := true
		for k, v := range p.Env {
			if first {
				addKV("Env", fmt.Sprintf("%s=%s", k, v))
				first = false
			} else {
				addKV("", fmt.Sprintf("%s=%s", k, v))
			}
		}
	} else {
		addKVc("Env", "-", Dim("-"))
	}
	tbl.Render(w)
}

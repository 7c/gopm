package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var (
	statsHours   int
	statsCPU     bool
	statsMem     bool
	statsUptime  bool
	statsAll     bool
)

var statsCmd = &cobra.Command{
	Use:   "stats [all|name|id]",
	Short: "Show historical metrics charts",
	Long: `Display terminal charts showing CPU, memory, uptime, and restart
history for managed processes. Data is collected every 60 seconds
and stored in memory for up to 18 hours.

When target is "all" (or omitted with multiple processes), each
metric chart overlays all processes with colored lines.
When target is a single process, its individual charts are shown.

If only one process is managed, the target can be omitted.`,
	Example: `  # Show all charts for all processes (default)
  gopm stats

  # Show charts for a specific process
  gopm stats my-api

  # Show only CPU chart, last 2 hours
  gopm stats --cpu --hours 2

  # Show only memory chart
  gopm stats --mem

  # JSON output (raw snapshot data)
  gopm stats --json`,
	Args: cobra.MaximumNArgs(1),
	Run:  runStats,
}

func init() {
	f := statsCmd.Flags()
	f.IntVar(&statsHours, "hours", 6, "hours of history to show (max 18)")
	f.BoolVar(&statsCPU, "cpu", false, "show only CPU chart")
	f.BoolVar(&statsMem, "mem", false, "show only memory chart")
	f.BoolVar(&statsUptime, "uptime", false, "show only uptime chart")
	f.BoolVar(&statsAll, "all", false, "show all charts (default)")
}

func runStats(cmd *cobra.Command, args []string) {
	target := ""
	if len(args) > 0 {
		target = args[0]
	} else {
		c, err := newClient()
		if err != nil {
			outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
		}
		resp, err := c.Send(protocol.MethodList, nil)
		c.Close()
		if err != nil {
			outputError(fmt.Sprintf("cannot list processes: %v", err))
		}
		if !resp.Success {
			outputError(resp.Error)
		}
		var procs []protocol.ProcessInfo
		if err := json.Unmarshal(resp.Data, &procs); err != nil {
			outputError(fmt.Sprintf("cannot parse process list: %v", err))
		}
		if len(procs) == 0 {
			outputError("no processes managed")
		}
		if len(procs) == 1 {
			target = procs[0].Name
		} else {
			target = "all"
		}
	}

	if statsHours < 1 {
		statsHours = 1
	}
	if statsHours > 18 {
		statsHours = 18
	}

	c, err := newClient()
	if err != nil {
		outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	resp, err := c.Send(protocol.MethodStats, protocol.StatsParams{
		Target: target,
		Hours:  statsHours,
	})
	if err != nil {
		outputError(fmt.Sprintf("failed to fetch stats: %v", err))
	}
	if !resp.Success {
		outputError(resp.Error)
	}

	if jsonOutput {
		outputJSON(resp.Data)
		return
	}

	var result protocol.StatsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		outputError(fmt.Sprintf("failed to parse stats: %v", err))
	}

	if len(result) == 0 {
		fmt.Println("No metrics data available yet (data is collected every 60s)")
		return
	}

	// Determine which charts to show.
	anyFilter := statsCPU || statsMem || statsUptime
	showCPU := statsAll || statsCPU || !anyFilter
	showMem := statsAll || statsMem || !anyFilter
	showUptime := statsAll || statsUptime || !anyFilter
	showRestarts := statsAll || !anyFilter

	if showCPU {
		renderMetricChart(os.Stdout, result, "CPU Usage", 60, 12,
			func(s protocol.MetricsSnapshot) float64 { return s.CPU },
			display.FormatCPUAxis)
	}
	if showMem {
		renderMetricChart(os.Stdout, result, "Memory (RSS)", 60, 12,
			func(s protocol.MetricsSnapshot) float64 { return float64(s.Memory) },
			display.FormatMemoryAxis)
	}
	if showUptime {
		renderMetricChart(os.Stdout, result, "Uptime", 60, 10,
			func(s protocol.MetricsSnapshot) float64 { return float64(s.UptimeSec) },
			display.FormatUptimeAxis)
	}
	if showRestarts {
		renderMetricChart(os.Stdout, result, "Restarts", 60, 8,
			func(s protocol.MetricsSnapshot) float64 { return float64(s.Restarts) },
			display.FormatRestartsAxis)
	}
}

// renderMetricChart builds ChartSeries from StatsResult and renders a chart.
func renderMetricChart(
	w io.Writer,
	data protocol.StatsResult,
	title string,
	width, height int,
	extractor func(protocol.MetricsSnapshot) float64,
	yFmt func(float64) string,
) {
	// Sort names for deterministic color assignment.
	names := make([]string, 0, len(data))
	for name := range data {
		names = append(names, name)
	}
	sort.Strings(names)

	var seriesList []display.ChartSeries
	for _, name := range names {
		snaps := data[name]
		points := make([]display.ChartPoint, len(snaps))
		for i, s := range snaps {
			points[i] = display.ChartPoint{
				Time:  s.Timestamp,
				Value: extractor(s),
			}
		}
		seriesList = append(seriesList, display.ChartSeries{
			Name:   name,
			Points: points,
		})
	}

	display.AssignSeriesColors(seriesList)
	display.RenderChart(w, display.ChartConfig{
		Title:      title,
		Width:      width,
		Height:     height,
		YFormatter: yFmt,
	}, seriesList)
}

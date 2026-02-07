package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var (
	watchInterval int
	watchPorts    bool
)

var watchCmd = &cobra.Command{
	Use:   "watch [name|id|all]",
	Short: "Live-updating process table",
	Long: `Display a live-updating process table that refreshes at a configurable interval.

Shows the same table as "gopm list" but refreshes automatically.
Use Ctrl+C to exit.

If only one process is managed, the target can be omitted.`,
	Example: `  # Watch all processes (updates every 1s)
  gopm watch

  # Watch a specific process
  gopm watch api

  # Update every 5 seconds
  gopm watch -i 5

  # Show ports column
  gopm watch -p

  # Stream JSON output
  gopm watch --json`,
	Args: cobra.MaximumNArgs(1),
	Run:  runWatch,
}

func init() {
	f := watchCmd.Flags()
	f.IntVarP(&watchInterval, "interval", "i", 1, "refresh interval in seconds (min: 1)")
	f.BoolVarP(&watchPorts, "ports", "p", false, "show listening ports column")
}

func runWatch(cmd *cobra.Command, args []string) {
	if watchInterval < 1 {
		watchInterval = 1
	}

	target := ""
	if len(args) > 0 {
		target = args[0]
	}

	// Determine if we're watching all or a specific process.
	showAll := target == "" || strings.EqualFold(target, "all")

	// If no target and not "all", infer single process.
	if target == "" {
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
		// If multiple processes, watch all. If one, watch that one.
		if len(procs) == 1 {
			target = procs[0].Name
			showAll = false
		} else {
			showAll = true
		}
	}

	// Set up signal handling.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Hide cursor (unless JSON mode).
	if !jsonOutput {
		fmt.Print("\033[?25l")
		// Ensure cursor is restored on exit.
		defer fmt.Print("\033[?25h")
	}

	ticker := time.NewTicker(time.Duration(watchInterval) * time.Second)
	defer ticker.Stop()

	// Run immediately on first tick, then on interval.
	render := func() {
		c, err := newClient()
		if err != nil {
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "%s %s\n", display.Red("Error:"), err)
			}
			return
		}
		defer c.Close()

		resp, err := c.Send(protocol.MethodList, nil)
		if err != nil {
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "%s %s\n", display.Red("Error:"), err)
			}
			return
		}
		if !resp.Success {
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "%s %s\n", display.Red("Error:"), resp.Error)
			}
			return
		}

		var procs []protocol.ProcessInfo
		if err := json.Unmarshal(resp.Data, &procs); err != nil {
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "%s %s\n", display.Red("Error:"), err)
			}
			return
		}

		// Filter to single process if needed.
		if !showAll {
			procs = filterProcs(procs, target)
		}

		if jsonOutput {
			data, _ := json.Marshal(procs)
			fmt.Println(string(data))
			return
		}

		// Clear screen and move cursor to top-left.
		fmt.Print("\033[2J\033[H")

		// Header.
		header := fmt.Sprintf("gopm watch — every %ds — %s",
			watchInterval, time.Now().Format("15:04:05"))
		if !showAll && target != "" {
			header = fmt.Sprintf("gopm watch %s — every %ds — %s",
				display.Bold(target), watchInterval, time.Now().Format("15:04:05"))
		}
		fmt.Println(display.Dim(header))
		fmt.Println()

		if len(procs) == 0 {
			fmt.Println("No processes found")
		} else {
			display.RenderProcessList(os.Stdout, procs, watchPorts)
		}

		// Footer.
		fmt.Println()
		fmt.Println(display.Dim("Press Ctrl+C to exit"))
	}

	// First render immediately.
	render()

	for {
		select {
		case <-sigCh:
			return
		case <-ticker.C:
			render()
		}
	}
}

// filterProcs filters a process list to match a target by name or ID.
func filterProcs(procs []protocol.ProcessInfo, target string) []protocol.ProcessInfo {
	var result []protocol.ProcessInfo
	for _, p := range procs {
		if p.Name == target {
			result = append(result, p)
		} else if id, err := strconv.Atoi(target); err == nil && p.ID == id {
			result = append(result, p)
		}
	}
	return result
}

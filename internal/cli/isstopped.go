package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

// isstoppedCmd answers "has a stop ever been requested on this process?" via
// its exit code, which is different from "is it stopped right now" — a
// process that was stopped then restarted still returns 0 here.
//
// The check is StopCount > 0, mirroring the daemon-side counter incremented
// every time Process.Stop() runs (user stop, restart-internal stop,
// delete-internal stop, daemon shutdown/reboot).
var isstoppedCmd = &cobra.Command{
	Use:   "isstopped <name|id>",
	Short: "Check if a stop was ever requested for a process (exit code based)",
	Long: `Check whether a Stop has ever been requested against a process during its
lifetime under the current daemon (StopCount > 0). This includes user stops,
restart-internal stops, delete-internal stops, and daemon shutdowns.

This does NOT test current status: a process that was stopped then restarted
still exits 0 here. Use isrunning/isonline for "online right now".

Exit codes:
  0  a Stop has been requested at least once (stop_count > 0)
  1  no Stop has ever been requested, or the process does not exist`,
	Example: `  # Has this process ever been stopped since the daemon came up?
  gopm isstopped api && echo "yes, at least once"

  # Structured answer with the actual count and last reason
  gopm isstopped api --json`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		target := args[0]

		c, err := newClient()
		if err != nil {
			outputError(err.Error())
		}
		defer c.Close()

		resp, err := c.Send(protocol.MethodIsRunning, protocol.TargetParams{Target: target})
		if err != nil {
			outputError(err.Error())
		}
		if !resp.Success {
			outputError(resp.Error)
		}

		var result protocol.IsRunningResult
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			outputError(fmt.Sprintf("failed to parse isstopped result: %s", err))
		}

		// Not found: empty status
		if result.Status == "" {
			if jsonOutput {
				outputJSON(resp.Data)
			} else {
				fmt.Printf("%s: %s\n", display.Bold(target), display.Dim("not found"))
			}
			os.Exit(1)
		}

		everStopped := result.StopCount > 0

		if jsonOutput {
			outputJSON(resp.Data)
		} else if everStopped {
			reason := result.StatusReason
			if reason == "" {
				reason = string(result.Status)
			}
			fmt.Printf("%s: %s (stop_count=%d, last reason: %s)\n",
				display.Bold(result.Name), display.Yellow("stopped at least once"),
				result.StopCount, reason)
		} else {
			fmt.Printf("%s: %s (%s, stop_count=0)\n",
				display.Bold(result.Name), display.Dim("never stopped"),
				display.StatusColor(string(result.Status)))
		}

		if everStopped {
			os.Exit(0)
		}
		os.Exit(1)
	},
}

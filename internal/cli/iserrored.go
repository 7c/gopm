package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

// iserroredCmd answers "is this process currently in the errored state?" via
// exit code. Unlike isstopped, this is a current-state check — errored means
// the supervisor gave up (max_restarts exceeded or an unrestartable exit) or
// a user-restart's Start half failed and left the process there.
var iserroredCmd = &cobra.Command{
	Use:   "iserrored <name|id>",
	Short: "Check if a process is currently errored (exit code based)",
	Long: `Check whether a process is currently in the errored state. A process reaches
errored when the supervisor gives up (max_restarts exceeded, or an exit code
filtered by no_restart_on_exit) or when a user-initiated restart's Start
half fails.

Exit codes:
  0  the process exists and its status is errored
  1  the process is in any other status, or does not exist`,
	Example: `  # Page if the process gave up
  gopm iserrored api && curl -X POST https://alerts/api/errored

  # Structured answer with the actual reason
  gopm iserrored api --json | jq '.status_reason'`,
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
			outputError(fmt.Sprintf("failed to parse iserrored result: %s", err))
		}

		if result.Status == "" {
			if jsonOutput {
				outputJSON(resp.Data)
			} else {
				fmt.Printf("%s: %s\n", display.Bold(target), display.Dim("not found"))
			}
			os.Exit(1)
		}

		errored := result.Status == protocol.StatusErrored

		if jsonOutput {
			outputJSON(resp.Data)
		} else if errored {
			reason := result.StatusReason
			if reason == "" {
				reason = "no reason recorded"
			}
			fmt.Printf("%s: %s (%s)\n",
				display.Bold(result.Name), display.Red("errored"), reason)
		} else {
			fmt.Printf("%s: %s (%s)\n",
				display.Bold(result.Name), display.Dim("not errored"),
				display.StatusColor(string(result.Status)))
		}

		if errored {
			os.Exit(0)
		}
		os.Exit(1)
	},
}

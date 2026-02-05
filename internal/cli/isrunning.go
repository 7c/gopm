package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var isrunningCmd = &cobra.Command{
	Use:   "isrunning <name|id>",
	Short: "Check if a process is running (exit code based)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		target := args[0]

		c, err := client.New()
		if err != nil {
			outputError(err.Error())
		}
		defer c.Close()

		resp, err := c.Send("isrunning", protocol.TargetParams{Target: target})
		if err != nil {
			outputError(err.Error())
		}
		if !resp.Success {
			outputError(resp.Error)
		}

		var result protocol.IsRunningResult
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			outputError(fmt.Sprintf("failed to parse isrunning result: %s", err))
		}

		// Not found: empty status
		if result.Status == "" {
			if jsonOutput {
				outputJSON(resp.Data)
			} else {
				fmt.Printf("%s: not found\n", target)
			}
			os.Exit(1)
		}

		if result.Running {
			if jsonOutput {
				outputJSON(resp.Data)
			} else {
				fmt.Printf("%s: online (PID %d, uptime %s)\n", result.Name, result.PID, result.Uptime)
			}
			os.Exit(0)
		}

		// Not running
		if jsonOutput {
			outputJSON(resp.Data)
		} else {
			fmt.Printf("%s: %s (exit code %d, %d restarts)\n", result.Name, result.Status, result.ExitCode, result.Restarts)
		}
		os.Exit(1)
	},
}

package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var pingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Check if daemon is running",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		c, err := client.NewWithConfig(configFlag)
		if err != nil {
			if jsonOutput {
				outputError("gopm daemon is not running")
			} else {
				fmt.Fprintln(os.Stderr, "gopm daemon is not running")
				os.Exit(1)
			}
		}
		defer c.Close()

		resp, err := c.Send("ping", nil)
		if err != nil {
			if jsonOutput {
				outputError("gopm daemon is not running")
			} else {
				fmt.Fprintln(os.Stderr, "gopm daemon is not running")
				os.Exit(1)
			}
		}
		if !resp.Success {
			outputError(resp.Error)
		}

		if jsonOutput {
			outputJSON(resp.Data)
			return
		}

		var result protocol.PingResult
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			outputError(fmt.Sprintf("failed to parse response: %v", err))
		}
		fmt.Printf("%s daemon %s (PID: %s, uptime: %s, version: %s)\n",
			display.Bold("gopm"), display.Green("running"),
			display.Cyan(fmt.Sprintf("%d", result.PID)), result.Uptime, display.Dim(result.Version))
	},
}

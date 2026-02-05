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

var describeCmd = &cobra.Command{
	Use:   "describe <name|id>",
	Short: "Show detailed info about a process",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		target := args[0]

		c, err := client.New()
		if err != nil {
			outputError(err.Error())
		}
		defer c.Close()

		resp, err := c.Send("describe", protocol.TargetParams{Target: target})
		if err != nil {
			outputError(err.Error())
		}
		if !resp.Success {
			outputError(resp.Error)
		}

		if jsonOutput {
			outputJSON(resp.Data)
			return
		}

		var proc protocol.ProcessInfo
		if err := json.Unmarshal(resp.Data, &proc); err != nil {
			outputError(fmt.Sprintf("failed to parse process info: %s", err))
		}

		display.RenderDescribe(os.Stdout, proc)
	},
}

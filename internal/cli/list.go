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

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all processes",
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		c, err := client.NewWithConfig(configFlag)
		if err != nil {
			outputError(err.Error())
		}
		defer c.Close()

		resp, err := c.Send("list", nil)
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

		var procs []protocol.ProcessInfo
		if err := json.Unmarshal(resp.Data, &procs); err != nil {
			outputError(fmt.Sprintf("failed to parse process list: %s", err))
		}

		if len(procs) == 0 {
			fmt.Println("No processes running")
			return
		}

		display.RenderProcessList(os.Stdout, procs)
	},
}

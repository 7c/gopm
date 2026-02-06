package cli

import (
	"encoding/json"
	"fmt"

	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var resurrectCmd = &cobra.Command{
	Use:   "resurrect",
	Short: "Restore previously saved processes",
	Args:  cobra.NoArgs,
	Run:   runResurrect,
}

func runResurrect(cmd *cobra.Command, args []string) {
	c, err := newClient()
	if err != nil {
		outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	resp, err := c.Send(protocol.MethodResurrect, nil)
	if err != nil {
		outputError(fmt.Sprintf("failed to resurrect processes: %v", err))
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
		outputError(fmt.Sprintf("failed to parse response: %v", err))
	}

	fmt.Printf("%s %d processes\n", display.Green("Resurrected"), len(procs))
}

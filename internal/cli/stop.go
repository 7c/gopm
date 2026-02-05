package cli

import (
	"fmt"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop <name|id|all>",
	Short: "Stop a running process",
	Args:  cobra.ExactArgs(1),
	Run:   runStop,
}

func runStop(cmd *cobra.Command, args []string) {
	target := args[0]

	c, err := client.New()
	if err != nil {
		exitError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	params := protocol.TargetParams{Target: target}
	resp, err := c.Send(protocol.MethodStop, params)
	if err != nil {
		exitError(fmt.Sprintf("failed to stop process: %v", err))
	}
	if !resp.Success {
		exitError(resp.Error)
	}

	if jsonOutput {
		fmt.Println(string(resp.Data))
	} else {
		fmt.Printf("Process %s %s\n", display.Bold(target), display.Yellow("stopped"))
	}
}

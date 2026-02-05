package cli

import (
	"fmt"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var flushCmd = &cobra.Command{
	Use:   "flush <name|id|all>",
	Short: "Clear log files",
	Args:  cobra.ExactArgs(1),
	Run:   runFlush,
}

func runFlush(cmd *cobra.Command, args []string) {
	target := args[0]

	c, err := client.New()
	if err != nil {
		outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	params := protocol.TargetParams{Target: target}
	resp, err := c.Send(protocol.MethodFlush, params)
	if err != nil {
		outputError(fmt.Sprintf("failed to flush logs: %v", err))
	}
	if !resp.Success {
		outputError(resp.Error)
	}

	if jsonOutput {
		outputJSON(resp.Data)
	} else {
		fmt.Printf("Logs flushed for %s\n", target)
	}
}

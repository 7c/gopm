package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart <name|id|all>",
	Short: "Restart a process",
	Args:  cobra.ExactArgs(1),
	Run:   runRestart,
}

func runRestart(cmd *cobra.Command, args []string) {
	target := args[0]

	c, err := newClient()
	if err != nil {
		exitError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	params := protocol.TargetParams{Target: target}
	resp, err := c.Send(protocol.MethodRestart, params)
	if err != nil {
		exitError(fmt.Sprintf("failed to restart process: %v", err))
	}
	if !resp.Success {
		exitError(resp.Error)
	}

	if jsonOutput {
		fmt.Println(string(resp.Data))
		return
	}

	// The daemon returns a single ProcessInfo when one process is restarted,
	// or an array when target is "all". Try single first, then array.
	var single protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &single); err == nil {
		display.RenderDescribe(os.Stdout, single)
		return
	}

	var multi []protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &multi); err == nil {
		display.RenderProcessList(os.Stdout, multi, false)
		return
	}

	// Fallback: just print the raw JSON data.
	fmt.Println(string(resp.Data))
}

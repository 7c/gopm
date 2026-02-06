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

var deleteCmd = &cobra.Command{
	Use:     "delete <name|id|all>",
	Aliases: []string{"del"},
	Short:   "Stop and remove a process from the list",
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		target := args[0]

		c, err := newClient()
		if err != nil {
			outputError(err.Error())
		}
		defer c.Close()

		resp, err := c.Send("delete", protocol.TargetParams{Target: target})
		if err != nil {
			outputError(err.Error())
		}
		if !resp.Success {
			outputError(resp.Error)
		}

		if jsonOutput {
			outputJSON(resp.Data)
		} else {
			fmt.Printf("Process %s %s\n", display.Bold(target), display.Yellow("deleted"))
		}
	},
}

// --- helpers ---

func outputJSON(data json.RawMessage) {
	fmt.Println(string(data))
}

func outputError(msg string) {
	if jsonOutput {
		fmt.Printf("{\"error\":%q}\n", msg)
	} else {
		fmt.Fprintf(os.Stderr, "%s %s\n", display.Red("Error:"), msg)
	}
	os.Exit(1)
}

// inferSingleProcess queries the daemon for the process list. If exactly one
// process exists, its name is returned. Otherwise an error is printed and the
// program exits.
func inferSingleProcess(c *client.Client) string {
	resp, err := c.Send(protocol.MethodList, nil)
	if err != nil {
		outputError(fmt.Sprintf("cannot list processes: %v", err))
	}
	if !resp.Success {
		outputError(resp.Error)
	}
	var procs []protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &procs); err != nil {
		outputError(fmt.Sprintf("cannot parse process list: %v", err))
	}
	if len(procs) == 0 {
		outputError("no processes managed — specify a target")
	}
	if len(procs) > 1 {
		outputError("multiple processes managed — specify a target")
	}
	return procs[0].Name
}

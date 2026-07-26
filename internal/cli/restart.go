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
	Use:   "restart <name|id|all> [name|id ...]",
	Short: "Restart one or more processes",
	Args:  cobra.MinimumNArgs(1),
	Run:   runRestart,
}

func runRestart(cmd *cobra.Command, args []string) {
	targets := normalizeTargets(args)

	c, err := newClient()
	if err != nil {
		exitError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	var restarted []protocol.ProcessInfo
	failures := 0

	for _, target := range targets {
		params := protocol.TargetParams{Target: target}
		resp, err := c.Send(protocol.MethodRestart, params)
		if err != nil {
			failures++
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "%s failed to restart %s: %v\n",
					display.Red("Error:"), display.Bold(target), err)
			}
			continue
		}
		if !resp.Success {
			failures++
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "%s %s: %s\n",
					display.Red("Error:"), display.Bold(target), resp.Error)
			}
			continue
		}
		restarted = append(restarted, unmarshalRestartResponse(resp.Data)...)
	}

	if jsonOutput {
		// Preserve the single-ProcessInfo shape when exactly one target was
		// given and one process restarted, so callers parsing the old JSON
		// keep working.
		if len(targets) == 1 && len(restarted) == 1 {
			data, _ := json.Marshal(restarted[0])
			fmt.Println(string(data))
		} else {
			data, _ := json.Marshal(restarted)
			fmt.Println(string(data))
		}
	} else {
		switch len(restarted) {
		case 0:
			// Nothing to render; any errors already printed to stderr.
		case 1:
			display.RenderDescribe(os.Stdout, restarted[0])
		default:
			display.RenderProcessList(os.Stdout, restarted, false)
		}
	}

	if failures > 0 {
		os.Exit(1)
	}
}

// unmarshalRestartResponse tolerates both shapes the daemon returns:
// a single ProcessInfo for one process, or an array for "all"/multiple.
func unmarshalRestartResponse(data json.RawMessage) []protocol.ProcessInfo {
	var single protocol.ProcessInfo
	if err := json.Unmarshal(data, &single); err == nil && single.Name != "" {
		return []protocol.ProcessInfo{single}
	}
	var multi []protocol.ProcessInfo
	if err := json.Unmarshal(data, &multi); err == nil {
		return multi
	}
	return nil
}

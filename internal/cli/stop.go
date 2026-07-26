package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop <name|id|all> [name|id ...]",
	Short: "Stop one or more running processes",
	Args:  cobra.MinimumNArgs(1),
	Run:   runStop,
}

type stopResult struct {
	Target  string `json:"target"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func runStop(cmd *cobra.Command, args []string) {
	targets := normalizeTargets(args)

	c, err := newClient()
	if err != nil {
		exitError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	results := make([]stopResult, 0, len(targets))
	failures := 0

	for _, target := range targets {
		params := protocol.TargetParams{Target: target}
		resp, err := c.Send(protocol.MethodStop, params)
		if err != nil {
			results = append(results, stopResult{Target: target, Error: err.Error()})
			failures++
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "%s failed to stop %s: %v\n",
					display.Red("Error:"), display.Bold(target), err)
			}
			continue
		}
		if !resp.Success {
			results = append(results, stopResult{Target: target, Error: resp.Error})
			failures++
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "%s %s: %s\n",
					display.Red("Error:"), display.Bold(target), resp.Error)
			}
			continue
		}
		results = append(results, stopResult{Target: target, Success: true})
		if !jsonOutput {
			fmt.Printf("Process %s %s\n", display.Bold(target), display.Yellow("stopped"))
		}
	}

	if jsonOutput {
		if len(targets) == 1 && results[0].Success {
			// Preserve single-target JSON shape (matches daemon response body).
			fmt.Println(`{"success":true}`)
		} else {
			data, _ := json.Marshal(results)
			fmt.Println(string(data))
		}
	}

	if failures > 0 {
		os.Exit(1)
	}
}

// normalizeTargets de-duplicates targets and short-circuits to ["all"]
// if any arg is "all" — mixing "all" with other names is unambiguous
// this way and avoids double-acting on the same process.
func normalizeTargets(args []string) []string {
	seen := make(map[string]struct{}, len(args))
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == "all" {
			return []string{"all"}
		}
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	return out
}

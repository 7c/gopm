package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

// Exit codes for isprocess. A caller must be able to tell "gopm does not know
// this process" apart from "gopm itself is unreachable" — conflating the two
// makes a wedged daemon look like a missing process.
const (
	exitProcessExists     = 0
	exitProcessNotFound   = 1
	exitDaemonUnreachable = 2
)

// isProcessOutput is the --json payload. It carries an explicit exists flag
// rather than making callers infer existence from an empty status string.
type isProcessOutput struct {
	Name   string          `json:"name"`
	Exists bool            `json:"exists"`
	Status protocol.Status `json:"status,omitempty"`
	PID    int             `json:"pid,omitempty"`
}

var isprocessCmd = &cobra.Command{
	Use:   "isprocess <name|id>",
	Short: "Check if a process exists in any state (exit code based)",
	Long: `Check whether a process is known to the daemon, regardless of whether it is
online, stopped, or errored. Use isrunning instead to test for "online".

Unlike most gopm commands, isprocess never starts the daemon: it is a read-only
query, and auto-starting would report "not found" against a fresh, empty daemon.

Exit codes:
  0  the process exists (online, stopped, or errored)
  1  the daemon is reachable but has no such process
  2  the daemon is not running or did not answer`,
	Example: `  # Provision a process only the first time
  gopm isprocess my-api || gopm start ./my-api --name my-api

  # Distinguish "no such process" from "gopm is broken"
  gopm isprocess my-api; case $? in
    0) echo "known" ;;
    1) echo "not defined" ;;
    2) echo "daemon down" >&2 ;;
  esac`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		target := args[0]

		// tryClient never auto-starts a daemon, unlike newClient.
		c, err := tryClient()
		if err != nil {
			exitUnreachable(err.Error())
		}
		defer c.Close()

		resp, err := c.Send(protocol.MethodIsRunning, protocol.TargetParams{Target: target})
		if err != nil {
			exitUnreachable(err.Error())
		}
		if !resp.Success {
			exitUnreachable(resp.Error)
		}

		var result protocol.IsRunningResult
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			exitUnreachable(fmt.Sprintf("failed to parse isprocess result: %s", err))
		}

		// The daemon reports an unknown process as a successful response with
		// an empty status, so any non-empty status means the process exists.
		exists := result.Status != ""
		name := result.Name
		if name == "" {
			name = target
		}

		if jsonOutput {
			printProcessJSON(isProcessOutput{
				Name:   name,
				Exists: exists,
				Status: result.Status,
				PID:    result.PID,
			})
		} else if !exists {
			fmt.Printf("%s: %s\n", display.Bold(name), display.Dim("not found"))
		} else if result.Running {
			fmt.Printf("%s: %s (PID %s)\n", display.Bold(name),
				display.StatusColor(string(result.Status)), display.Cyan(fmt.Sprintf("%d", result.PID)))
		} else {
			fmt.Printf("%s: %s\n", display.Bold(name), display.StatusColor(string(result.Status)))
		}

		if exists {
			os.Exit(exitProcessExists)
		}
		os.Exit(exitProcessNotFound)
	},
}

func printProcessJSON(out isProcessOutput) {
	data, err := json.Marshal(out)
	if err != nil {
		exitUnreachable(fmt.Sprintf("failed to encode isprocess result: %s", err))
	}
	fmt.Println(string(data))
}

// exitUnreachable mirrors outputError but exits with exitDaemonUnreachable, so
// callers can distinguish a broken daemon from a missing process.
func exitUnreachable(msg string) {
	if jsonOutput {
		fmt.Printf("{\"error\":%q}\n", msg)
	} else {
		fmt.Fprintf(os.Stderr, "%s %s\n", display.Red("Error:"), msg)
	}
	os.Exit(exitDaemonUnreachable)
}

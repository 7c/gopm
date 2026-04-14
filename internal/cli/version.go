package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/7c/gopm/internal/display"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show gopm CLI and daemon versions",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		daemonPing := getDaemonPing()

		if jsonOutput {
			out := map[string]interface{}{
				"cli_version": Version,
			}
			if daemonPing != nil {
				out["daemon_version"] = daemonPing.Version
				out["daemon_pid"] = daemonPing.PID
				out["version_mismatch"] = Version != "dev" && Version != "" &&
					daemonPing.Version != "" && daemonPing.Version != Version
			} else {
				out["daemon_version"] = nil
				out["version_mismatch"] = false
			}
			data, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(data))
			return
		}

		fmt.Printf("CLI binary:   version %s\n", Version)
		if daemonPing != nil {
			daemonVersion := daemonPing.Version
			if Version != "dev" && Version != "" && daemonVersion != "" && daemonVersion != Version {
				daemonVersion = display.Red(daemonVersion + " (stale!)")
			}
			fmt.Printf("Daemon:       version %s (PID %d)\n", daemonVersion, daemonPing.PID)
			if msg := versionMismatchWarning(); msg != "" {
				fmt.Fprintln(os.Stdout, msg)
			}
		} else {
			fmt.Printf("Daemon:       %s\n", display.Dim("(not running)"))
		}
	},
}

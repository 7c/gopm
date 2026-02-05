//go:build !linux

package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var pidCmd = &cobra.Command{
	Use:   "pid <pid>",
	Short: "Inspect any process by PID (Linux only)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stderr, "Error: gopm pid is only available on Linux (requires /proc)")
		os.Exit(1)
	},
}

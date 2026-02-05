package cli

import (
	"fmt"
	"os"

	"github.com/7c/gopm/internal/client"
	"github.com/spf13/cobra"
)

var killCmd = &cobra.Command{
	Use:   "kill",
	Short: "Kill the daemon",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		c, err := client.New()
		if err != nil {
			if jsonOutput {
				outputError("gopm daemon is not running")
			} else {
				fmt.Fprintln(os.Stderr, "gopm daemon is not running")
				os.Exit(1)
			}
		}
		defer c.Close()

		_, err = c.Send("kill", nil)
		if err != nil {
			if jsonOutput {
				outputError("gopm daemon is not running")
			} else {
				fmt.Fprintln(os.Stderr, "gopm daemon is not running")
				os.Exit(1)
			}
		}

		fmt.Println("gopm daemon stopped")
	},
}

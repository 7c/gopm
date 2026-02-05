package cli

import (
	"time"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/gui"
	"github.com/spf13/cobra"
)

var guiRefreshRate time.Duration

var guiCmd = &cobra.Command{
	Use:   "gui",
	Short: "Launch interactive terminal UI",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		c, err := client.NewWithConfig(configFlag)
		if err != nil {
			outputError(err.Error())
		}
		defer c.Close()

		if err := gui.Run(c, guiRefreshRate); err != nil {
			outputError(err.Error())
		}
	},
}

func init() {
	guiCmd.Flags().DurationVar(&guiRefreshRate, "refresh", 1*time.Second, "refresh interval")
}

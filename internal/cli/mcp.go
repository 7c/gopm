package cli

import (
	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/mcp"
	"github.com/spf13/cobra"
)

var mcpTransport string

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP (Model Context Protocol) server",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		c, err := client.New()
		if err != nil {
			outputError(err.Error())
		}
		defer c.Close()

		if err := mcp.Run(c); err != nil {
			outputError(err.Error())
		}
	},
}

func init() {
	mcpCmd.Flags().StringVar(&mcpTransport, "transport", "stdio", "transport type")
}

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// defaultConfig is the full gopm.config.json with all keys and their defaults.
// Comments are not valid JSON, so we use descriptive field names and values.
const defaultConfig = `{
  "logs": {
    "directory": "~/.gopm/logs",
    "max_size": "1M",
    "max_files": 3
  },
  "mcpserver": {
    "device": [],
    "port": 18999,
    "uri": "/mcp"
  },
  "telemetry": {
    "telegraf": {
      "udp": "127.0.0.1:8094",
      "measurement": "gopm"
    }
  }
}`

var newconfigCmd = &cobra.Command{
	Use:   "newconfig",
	Short: "Print a sample gopm.config.json with all defaults",
	Long: `Print a complete gopm.config.json showing every available option
with its default value. Redirect to a file to bootstrap your config:

  gopm newconfig > ~/.gopm/gopm.config.json

Then edit the file to your needs. Set a section to null to disable it:

  "mcpserver": null      — disable the MCP HTTP server
  "telemetry": null      — disable telegraf telemetry

Omitting a section entirely uses defaults (MCP enabled on 0.0.0.0:18999).

Device list for mcpserver: IP addresses, interface names, or "localhost".
An empty list binds to all interfaces (0.0.0.0).`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(defaultConfig)
	},
}

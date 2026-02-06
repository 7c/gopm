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

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Print a sample gopm.config.json with all defaults",
	Long: `Print a complete gopm.config.json showing every available option
with its default value. Redirect to a file to bootstrap your config:

  gopm config > ~/.gopm/gopm.config.json

Then edit the file to your needs. Set a section to null to disable it:

  "mcpserver": null      — disable the MCP HTTP server
  "telemetry": null      — disable telegraf telemetry

Omitting a section entirely uses defaults (MCP enabled on 127.0.0.1:18999).

Device list for mcpserver: IP addresses, interface names, or "localhost".
An empty list binds to localhost (127.0.0.1) only.

Config file search order:
  1. --config <path>              explicit flag (CLI and daemon)
  2. ~/.gopm/gopm.config.json     user home directory
  3. /etc/gopm.config.json        system-wide
  4. (no file)                    built-in defaults`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(defaultConfig)
	},
}

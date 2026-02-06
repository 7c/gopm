package cli

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/config"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

// defaultConfig is the full gopm.config.json with all keys and their defaults.
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

var exportNew bool

var exportCmd = &cobra.Command{
	Use:   "export [all|name|id...]",
	Short: "Export process config or print sample gopm.config.json",
	Long: `Export running processes as an ecosystem JSON file, or print a sample
gopm.config.json with all defaults.

Export processes (pipe to a file to save):

  gopm export all               # all processes
  gopm export api               # single process by name
  gopm export 0 1 2             # multiple processes by ID
  gopm export api worker        # multiple processes by name
  gopm export all > ecosystem.json
  gopm start ecosystem.json     # re-launch from exported config

Print a sample gopm.config.json with all defaults:

  gopm export --new
  gopm export -n > ~/.gopm/gopm.config.json

Set a section to null to disable it:

  "mcpserver": null      — disable the MCP HTTP server
  "telemetry": null      — disable telegraf telemetry

Omitting a section entirely uses defaults (MCP enabled on 127.0.0.1:18999).

Config file search order:
  1. --config <path>              explicit flag (CLI and daemon)
  2. ~/.gopm/gopm.config.json     user home directory
  3. /etc/gopm.config.json        system-wide
  4. (no file)                    built-in defaults`,
	Args: cobra.ArbitraryArgs,
	Run:  runExport,
}

func init() {
	exportCmd.Flags().BoolVarP(&exportNew, "new", "n", false, "print sample gopm.config.json with all defaults")
}

func runExport(cmd *cobra.Command, args []string) {
	if exportNew {
		fmt.Println(defaultConfig)
		return
	}

	if len(args) == 0 {
		cmd.Help()
		return
	}

	// Connect to daemon and get process list.
	c, err := client.NewWithConfig(configFlag)
	if err != nil {
		outputError(err.Error())
	}
	defer c.Close()

	resp, err := c.Send(protocol.MethodList, nil)
	if err != nil {
		outputError(err.Error())
	}
	if !resp.Success {
		outputError(resp.Error)
	}

	var procs []protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &procs); err != nil {
		outputError(fmt.Sprintf("failed to parse process list: %s", err))
	}

	if len(procs) == 0 {
		outputError("no processes defined")
	}

	// Filter processes by targets.
	var selected []protocol.ProcessInfo
	if len(args) == 1 && args[0] == "all" {
		selected = procs
	} else {
		byName := make(map[string]protocol.ProcessInfo)
		byID := make(map[int]protocol.ProcessInfo)
		for _, p := range procs {
			byName[p.Name] = p
			byID[p.ID] = p
		}
		seen := make(map[string]bool)
		for _, target := range args {
			// Try by ID first.
			if id, err := strconv.Atoi(target); err == nil {
				if p, ok := byID[id]; ok {
					if !seen[p.Name] {
						selected = append(selected, p)
						seen[p.Name] = true
					}
					continue
				}
			}
			// Try by name.
			if p, ok := byName[target]; ok {
				if !seen[p.Name] {
					selected = append(selected, p)
					seen[p.Name] = true
				}
				continue
			}
			exitError(fmt.Sprintf("process %q not found", target))
		}
	}

	if len(selected) == 0 {
		outputError("no matching processes found")
	}

	// Convert to ecosystem format.
	eco := config.EcosystemConfig{
		Apps: make([]config.AppConfig, 0, len(selected)),
	}
	for _, p := range selected {
		eco.Apps = append(eco.Apps, processToAppConfig(p))
	}

	data, err := json.MarshalIndent(eco, "", "  ")
	if err != nil {
		outputError(fmt.Sprintf("failed to marshal config: %s", err))
	}
	fmt.Println(string(data))
}

// processToAppConfig converts a running ProcessInfo back to an AppConfig
// suitable for an ecosystem JSON file.
func processToAppConfig(p protocol.ProcessInfo) config.AppConfig {
	app := config.AppConfig{
		Name:    p.Name,
		Command: p.Command,
	}

	if len(p.Args) > 0 {
		app.Args = p.Args
	}
	if p.Cwd != "" {
		app.Cwd = p.Cwd
	}
	if p.Interpreter != "" {
		app.Interpreter = p.Interpreter
	}
	if len(p.Env) > 0 {
		app.Env = p.Env
	}

	// Restart policy — include non-default values.
	defaults := protocol.DefaultRestartPolicy()
	rp := p.RestartPolicy

	if rp.AutoRestart != defaults.AutoRestart {
		app.AutoRestart = string(rp.AutoRestart)
	}
	if rp.MaxRestarts != defaults.MaxRestarts {
		mr := rp.MaxRestarts
		app.MaxRestarts = &mr
	}
	if rp.MinUptime.Duration != defaults.MinUptime.Duration {
		app.MinUptime = rp.MinUptime.Duration.String()
	}
	if rp.RestartDelay.Duration != defaults.RestartDelay.Duration {
		app.RestartDelay = rp.RestartDelay.Duration.String()
	}
	if rp.ExpBackoff != defaults.ExpBackoff {
		app.ExpBackoff = rp.ExpBackoff
	}
	if rp.MaxDelay.Duration != defaults.MaxDelay.Duration {
		app.MaxDelay = rp.MaxDelay.Duration.String()
	}
	if rp.KillTimeout.Duration != defaults.KillTimeout.Duration {
		app.KillTimeout = rp.KillTimeout.Duration.String()
	}

	return app
}

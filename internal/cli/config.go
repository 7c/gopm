package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/config"
	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var configValidate bool

var configShowCmd = &cobra.Command{
	Use:   "config",
	Short: "Show resolved configuration",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		home := protocol.GopmHome()
		result, err := config.Load(home, configFlag)
		if err != nil {
			exitError(err.Error())
		}
		resolved, warnings, err := config.Resolve(result.Config, home)
		if err != nil {
			exitError(err.Error())
		}

		if configValidate {
			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "WARNING: %s\n", w)
			}
			fmt.Println("Configuration valid")
			return
		}

		// Try to query the running daemon for its config
		daemonConfigFile, daemonConfigSource := getDaemonConfig()

		if jsonOutput {
			out := map[string]interface{}{
				"config_file": result.Path,
				"source":      result.Source,
				"logs": map[string]interface{}{
					"directory": resolved.LogDir,
					"max_size":  resolved.LogMaxSize,
					"max_files": resolved.LogMaxFiles,
				},
				"mcp_enabled": resolved.MCPEnabled,
				"mcp_uri":     resolved.MCPURI,
				"telegraf":    resolved.TelegrafEnabled,
			}
			if resolved.MCPEnabled {
				var binds []string
				for _, ba := range resolved.MCPBindAddrs {
					binds = append(binds, ba.Addr)
				}
				out["mcp_bind"] = binds
			}
			if resolved.TelegrafEnabled && resolved.TelegrafAddr != nil {
				out["telegraf_addr"] = resolved.TelegrafAddr.String()
				out["telegraf_measurement"] = resolved.TelegrafMeas
			}
			if daemonConfigFile != "" {
				out["daemon_config_file"] = daemonConfigFile
				out["daemon_config_source"] = daemonConfigSource
			}
			data, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(data))
			return
		}

		// Human-friendly output
		configLine := "(none found, using defaults)"
		if result.Path != "" {
			configLine = fmt.Sprintf("%s (%s)", result.Path, result.Source)
		}
		fmt.Printf("Config file:  %s\n", configLine)

		// Show daemon config if available
		if daemonConfigFile != "" {
			daemonLine := fmt.Sprintf("%s (%s)", daemonConfigFile, daemonConfigSource)
			fmt.Printf("Daemon using: %s\n", daemonLine)
		} else if daemonConfigFile == "" && daemonConfigSource == "daemon-running" {
			fmt.Printf("Daemon using: %s\n", "(defaults, no config file)")
		} else {
			fmt.Printf("Daemon:       %s\n", display.Dim("(not running)"))
		}
		fmt.Println()

		fmt.Printf("%s\n", display.Bold("Logs:"))
		fmt.Printf("  Directory:    %s\n", resolved.LogDir)
		fmt.Printf("  Max size:     %s\n", protocol.FormatBytes(uint64(resolved.LogMaxSize)))
		fmt.Printf("  Max files:    %d\n\n", resolved.LogMaxFiles)

		fmt.Printf("%s\n", display.Bold("MCP HTTP Server:"))
		if resolved.MCPEnabled {
			var binds []string
			for _, ba := range resolved.MCPBindAddrs {
				binds = append(binds, fmt.Sprintf("%s (%s)", ba.Addr, ba.Label))
			}
			fmt.Printf("  Enabled:      yes\n")
			fmt.Printf("  Bind:         %s\n", fmt.Sprintf("%v", binds))
			fmt.Printf("  URI:          %s\n\n", resolved.MCPURI)
		} else {
			fmt.Printf("  Enabled:      no (disabled in config)\n\n")
		}

		fmt.Printf("%s\n", display.Bold("Telemetry:"))
		if resolved.TelegrafEnabled && resolved.TelegrafAddr != nil {
			fmt.Printf("  Telegraf:     enabled\n")
			fmt.Printf("  UDP:          %s\n", resolved.TelegrafAddr.String())
			fmt.Printf("  Measurement:  %s\n", resolved.TelegrafMeas)
		} else {
			fmt.Printf("  Telegraf:     disabled\n")
		}

		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "\nWARNING: %s\n", w)
		}
	},
}

func init() {
	configShowCmd.Flags().BoolVar(&configValidate, "validate", false, "validate config only")
}

// getDaemonConfig queries the running daemon for its config file.
// Returns ("", "") if daemon is not running.
// Returns ("", "daemon-running") if daemon is running but has no config file.
func getDaemonConfig() (configFile, configSource string) {
	c, err := client.TryConnect(configFlag)
	if err != nil {
		return "", ""
	}
	defer c.Close()

	resp, err := c.Send(protocol.MethodPing, nil)
	if err != nil || !resp.Success {
		return "", ""
	}

	var ping protocol.PingResult
	if err := json.Unmarshal(resp.Data, &ping); err != nil {
		return "", ""
	}

	if ping.ConfigFile == "" {
		return "", "daemon-running"
	}
	return ping.ConfigFile, ping.ConfigSource
}

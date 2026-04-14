package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/7c/gopm/internal/config"
	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var statusValidate bool

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status and resolved configuration",
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

		if statusValidate {
			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "WARNING: %s\n", w)
			}
			fmt.Println("Configuration valid")
			return
		}

		// Try to query the running daemon
		daemonPing := getDaemonPing()

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
			out["cli_version"] = Version
			if daemonPing != nil {
				out["daemon_pid"] = daemonPing.PID
				out["daemon_uptime"] = daemonPing.UptimeMs
				out["daemon_uptime_str"] = daemonPing.Uptime
				out["daemon_version"] = daemonPing.Version
				out["version_mismatch"] = Version != "dev" && Version != "" &&
					daemonPing.Version != "" && daemonPing.Version != Version
				if daemonPing.ConfigFile != "" {
					out["daemon_config_file"] = daemonPing.ConfigFile
					out["daemon_config_source"] = daemonPing.ConfigSource
				}
			}
			out["systemd_unit_file"] = unitFilePath
			out["systemd_installed"] = isSystemdInstalled()
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

		// Show daemon info
		if daemonPing != nil {
			if daemonPing.ConfigFile != "" {
				fmt.Printf("Daemon using: %s (%s)\n", daemonPing.ConfigFile, daemonPing.ConfigSource)
			} else {
				fmt.Printf("Daemon using: %s\n", "(defaults, no config file)")
			}
			// Highlight the daemon version in red if it differs from the
			// CLI binary version — stale daemon after a gopm upgrade.
			daemonVersion := daemonPing.Version
			if Version != "dev" && Version != "" && daemonVersion != "" && daemonVersion != Version {
				daemonVersion = display.Red(daemonVersion + " (stale!)")
			}
			fmt.Printf("Daemon:       PID %s, uptime %s, version %s\n",
				display.Cyan(fmt.Sprintf("%d", daemonPing.PID)),
				display.Bold(daemonPing.Uptime),
				daemonVersion)
			fmt.Printf("CLI binary:   version %s\n", Version)
			if msg := versionMismatchWarning(); msg != "" {
				fmt.Println(msg)
			}
		} else {
			fmt.Printf("Daemon:       %s\n", display.Dim("(not running)"))
			fmt.Printf("CLI binary:   version %s\n", Version)
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

		fmt.Printf("\n%s\n", display.Bold("Systemd:"))
		fmt.Printf("  Unit file:    %s\n", unitFilePath)
		if isSystemdInstalled() {
			fmt.Printf("  Installed:    %s\n", display.Green("yes"))
		} else {
			fmt.Printf("  Installed:    %s\n", display.Dim("no"))
		}

		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "\nWARNING: %s\n", w)
		}
	},
}

func init() {
	statusCmd.Flags().BoolVar(&statusValidate, "validate", false, "validate config only")
}

// isSystemdInstalled checks if gopm is installed as a systemd service.
func isSystemdInstalled() bool {
	_, err := os.Stat(unitFilePath)
	return err == nil
}

// getDaemonPing queries the running daemon for its status.
// Returns nil if the daemon is not running or unreachable.
func getDaemonPing() *protocol.PingResult {
	c, err := tryClient()
	if err != nil {
		return nil
	}
	defer c.Close()

	resp, err := c.Send(protocol.MethodPing, nil)
	if err != nil || !resp.Success {
		return nil
	}

	var ping protocol.PingResult
	if err := json.Unmarshal(resp.Data, &ping); err != nil {
		return nil
	}
	return &ping
}

// isVersionMismatch returns true when the CLI binary and daemon versions
// are both known and differ. Pure function so it's easy to unit-test.
func isVersionMismatch(cliVersion, daemonVersion string) bool {
	if cliVersion == "" || cliVersion == "dev" {
		return false
	}
	if daemonVersion == "" {
		return false
	}
	return cliVersion != daemonVersion
}

// formatVersionMismatchWarning returns a red warning string for the given
// mismatched version pair. Separated from the live ping for testability.
func formatVersionMismatchWarning(cliVersion, daemonVersion string) string {
	return display.Red(fmt.Sprintf(
		"WARNING: gopm CLI version %s != daemon version %s — restart the daemon to pick up the new binary (gopm reboot)",
		cliVersion, daemonVersion))
}

// versionMismatchWarning returns a red warning string if the CLI binary
// version differs from the running daemon version, or "" if they match or
// the daemon isn't running. The daemon is queried via tryClient so this
// never auto-starts a daemon as a side effect.
func versionMismatchWarning() string {
	ping := getDaemonPing()
	if ping == nil {
		return ""
	}
	if !isVersionMismatch(Version, ping.Version) {
		return ""
	}
	return formatVersionMismatchWarning(Version, ping.Version)
}

// printVersionMismatchWarning prints a version-mismatch warning (if any) to
// the given writer. Safe to call from any command.
func printVersionMismatchWarning(w *os.File) {
	if msg := versionMismatchWarning(); msg != "" {
		fmt.Fprintln(w, msg)
	}
}

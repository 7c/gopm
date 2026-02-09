package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/daemon"
	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

// configFlag is the global --config flag for specifying a config file.
var configFlag string

// Version is set at build time via ldflags.
var Version = "dev"

// jsonOutput is the global flag for JSON output mode.
var jsonOutput bool

// debugOutput is the global flag for debug logging.
var debugOutput bool

var rootCmd = &cobra.Command{
	Use:   "gopm",
	Short: display.CBold + "GoPM" + display.CReset + " — Lightweight Process Manager",
	Run:   runRoot,
}

// coloredHelpTemplate is the Cobra help template with ANSI colors.
var coloredHelpTemplate = `{{with .Long}}{{. | trimTrailingWhitespaces}}

{{end}}` +
	`{{if or .Runnable .HasSubCommands}}` + display.CYellow + `Usage:` + display.CReset + `{{end}}
{{if .Runnable}}  {{.UseLine}}{{end}}` +
	`{{if .HasAvailableSubCommands}}  {{.CommandPath}} [command]{{end}}

` +
	`{{if gt (len .Aliases) 0}}` + display.CYellow + `Aliases:` + display.CReset + `
  {{.NameAndAliases}}

{{end}}` +
	`{{if .HasExample}}` + display.CYellow + `Examples:` + display.CReset + `
{{.Example}}

{{end}}` +
	`{{if .HasAvailableSubCommands}}` + display.CYellow + `Available Commands:` + display.CReset + `{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  ` + display.CCyan + `{{rpad .Name .NamePadding}}` + display.CReset + `  {{.Short}}{{end}}{{end}}

{{end}}` +
	`{{if .HasAvailableLocalFlags}}` + display.CYellow + `Flags:` + display.CReset + `
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}

{{end}}` +
	`{{if .HasAvailableInheritedFlags}}` + display.CYellow + `Global Flags:` + display.CReset + `
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}

{{end}}` +
	`{{if .HasAvailableSubCommands}}Use "{{.CommandPath}} [command] --help" for more information about a command.
{{end}}`

// runRoot is called when gopm is invoked without a subcommand.
// If the daemon has processes, show the list; otherwise show help.
func runRoot(cmd *cobra.Command, args []string) {
	c, err := client.TryConnect(configFlag)
	if err != nil {
		cmd.Help()
		return
	}
	defer c.Close()

	resp, err := c.Send(protocol.MethodList, nil)
	if err != nil || !resp.Success {
		cmd.Help()
		return
	}

	var procs []protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &procs); err != nil || len(procs) == 0 {
		cmd.Help()
		return
	}

	if jsonOutput {
		outputJSON(resp.Data)
		return
	}
	display.RenderProcessList(os.Stdout, procs, false)
}

// Execute sets up the root command, registers all subcommands, and runs cobra.
func Execute() {
	// Check for --daemon flag before cobra parses anything.
	isDaemon := false
	daemonDebug := false
	daemonConfigFlag := ""
	for i, arg := range os.Args[1:] {
		if arg == "--daemon" {
			isDaemon = true
		}
		if arg == "--debug" {
			daemonDebug = true
		}
		if arg == "--config" && i+1 < len(os.Args[1:]) {
			daemonConfigFlag = os.Args[i+2]
		}
	}
	if isDaemon {
		daemon.Run(Version, daemonConfigFlag, daemonDebug)
		return // never reached; daemon.Run calls os.Exit
	}

	rootCmd.Version = Version
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&debugOutput, "debug", false, "enable debug logging")
	rootCmd.PersistentFlags().StringVar(&configFlag, "config", "", "path to gopm.config.json")

	// Apply colored help template globally.
	rootCmd.SetHelpTemplate(coloredHelpTemplate)

	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(describeCmd)
	rootCmd.AddCommand(isrunningCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(flushCmd)
	rootCmd.AddCommand(resurrectCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(pingCmd)
	rootCmd.AddCommand(killCmd)
	rootCmd.AddCommand(rebootCmd)
	rootCmd.AddCommand(guiCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(suspendCmd)
	rootCmd.AddCommand(unsuspendCmd)
	rootCmd.AddCommand(pidCmd)
	rootCmd.AddCommand(pm2Cmd)
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(statsCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// debugLog prints a debug message to stderr when --debug is enabled.
func debugLog(format string, args ...interface{}) {
	if debugOutput {
		fmt.Fprintf(os.Stderr, "[debug] "+format+"\n", args...)
	}
}

// newClient creates a client with the global config and debug flags applied.
func newClient() (*client.Client, error) {
	c, err := client.NewWithConfig(configFlag)
	if err != nil {
		return nil, err
	}
	if debugOutput {
		c.SetDebug(true)
	}
	return c, nil
}

// tryClient connects to an existing daemon (no auto-start) with debug flags.
func tryClient() (*client.Client, error) {
	c, err := client.TryConnect(configFlag)
	if err != nil {
		return nil, err
	}
	if debugOutput {
		c.SetDebug(true)
	}
	return c, nil
}

// exitError prints an error message and exits. When jsonOutput is set, it
// writes a JSON object to stdout; otherwise it prints to stderr.
func exitError(msg string) {
	if jsonOutput {
		fmt.Fprintf(os.Stdout, "{\"error\":%q}\n", msg)
	} else {
		fmt.Fprintf(os.Stderr, "%s %s\n", display.Red("Error:"), msg)
	}
	os.Exit(1)
}

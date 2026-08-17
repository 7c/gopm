package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

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

// Command groups for `gopm --help`, ordered as they render. Commands are
// grouped by what they act on so an operation can be found by scanning for its
// kind rather than reading one flat list.
const (
	groupProcess = "process"
	groupDaemon  = "daemon"
	groupConfig  = "config"
	groupTool    = "tool"
)

// commandGroups is the render order of the help sections.
var commandGroups = []*cobra.Group{
	{ID: groupProcess, Title: display.CYellow + "Process Management:" + display.CReset},
	{ID: groupDaemon, Title: display.CYellow + "Daemon Management:" + display.CReset},
	{ID: groupConfig, Title: display.CYellow + "Configuration & State:" + display.CReset},
	{ID: groupTool, Title: display.CYellow + "Tools & Diagnostics:" + display.CReset},
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
	`{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}` + display.CYellow + `Available Commands:` + display.CReset + `{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  ` + display.CCyan + `{{rpad .Name .NamePadding}}` + display.CReset + `  {{.Short}}{{end}}{{end}}
{{else}}{{range $group := .Groups}}{{$group.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  ` + display.CCyan + `{{rpad .Name .NamePadding}}` + display.CReset + `  {{.Short}}{{end}}{{end}}

{{end}}{{if not .AllChildCommandsHaveGroup}}` + display.CYellow + `Additional Commands:` + display.CReset + `{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  ` + display.CCyan + `{{rpad .Name .NamePadding}}` + display.CReset + `  {{.Short}}{{end}}{{end}}
{{end}}{{end}}
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
	printVersionMismatchWarning(os.Stdout)
}

// newRootCommand builds the root command with its groups and subcommands.
// Kept separate from Execute so tests can render help without os.Exit.
func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "gopm",
		Short: display.CBold + "GoPM" + display.CReset + " — Lightweight Process Manager",
		Run:   runRoot,
	}
	root.SetHelpTemplate(coloredHelpTemplate)
	root.AddGroup(commandGroups...)

	// Groups must exist before AddCommand: cobra panics on an unknown GroupID.
	// Assigning here rather than in each command file keeps the taxonomy in one
	// place, and avoids duplicating it across pid.go / pid_stub.go build tags.
	add := func(groupID string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = groupID
			root.AddCommand(c)
		}
	}
	add(groupProcess, startCmd, stopCmd, restartCmd, deleteCmd, listCmd, describeCmd,
		isrunningCmd, isprocessCmd, isstoppedCmd, logsCmd, flushCmd, watchCmd, statsCmd)
	add(groupDaemon, pingCmd, statusCmd, killCmd, rebootCmd,
		installCmd, uninstallCmd, suspendCmd, unsuspendCmd)
	add(groupConfig, exportCmd, importCmd, resurrectCmd, pm2Cmd)
	add(groupTool, guiCmd, pidCmd, versionCmd, docsCmd)

	// Cobra generates help and completion itself; give them a group so they do
	// not land in the Additional Commands fallback.
	root.SetHelpCommandGroupID(groupTool)
	root.SetCompletionCommandGroupID(groupTool)

	return root
}

// Execute sets up the root command, registers all subcommands, and runs cobra.
func Execute() {
	// Check for --daemon flag before cobra parses anything.
	isDaemon := false
	daemonDebug := false
	daemonConfigFlag := ""
	daemonLogLevel := ""
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
		if arg == "--log-level" && i+1 < len(os.Args[1:]) {
			daemonLogLevel = os.Args[i+2]
		}
		if strings.HasPrefix(arg, "--log-level=") {
			daemonLogLevel = strings.TrimPrefix(arg, "--log-level=")
		}
	}
	if isDaemon {
		daemon.Run(Version, daemonConfigFlag, daemonDebug, daemonLogLevel)
		return // never reached; daemon.Run calls os.Exit
	}

	rootCmd := newRootCommand()
	rootCmd.Version = Version
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&debugOutput, "debug", false, "enable debug logging")
	rootCmd.PersistentFlags().StringVar(&configFlag, "config", "", "path to gopm.config.json")

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

package cli

import (
	"fmt"
	"os"

	"github.com/7c/gopm/internal/daemon"
	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags.
var Version = "dev"

// jsonOutput is the global flag for JSON output mode.
var jsonOutput bool

var rootCmd = &cobra.Command{
	Use:   "gopm",
	Short: "GoPM — Lightweight Process Manager",
}

// Execute sets up the root command, registers all subcommands, and runs cobra.
func Execute() {
	// Check for --daemon flag before cobra parses anything.
	// This allows the binary to fork into daemon mode.
	for _, arg := range os.Args[1:] {
		if arg == "--daemon" {
			daemon.Run(Version)
			return // never reached; daemon.Run calls os.Exit
		}
	}

	rootCmd.Version = Version
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")

	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(describeCmd)
	rootCmd.AddCommand(isrunningCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(flushCmd)
	rootCmd.AddCommand(saveCmd)
	rootCmd.AddCommand(resurrectCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(pingCmd)
	rootCmd.AddCommand(killCmd)
	rootCmd.AddCommand(guiCmd)
	rootCmd.AddCommand(mcpCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// exitError prints an error message and exits. When jsonOutput is set, it
// writes a JSON object to stdout; otherwise it prints to stderr.
func exitError(msg string) {
	if jsonOutput {
		fmt.Fprintf(os.Stdout, "{\"error\":%q}\n", msg)
	} else {
		fmt.Fprintln(os.Stderr, "Error:", msg)
	}
	os.Exit(1)
}

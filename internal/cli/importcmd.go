package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/7c/gopm/internal/config"
	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:   "import <gopm.process> [more files...]",
	Short: "Import processes from one or more JSON files",
	Long: `Import processes from one or more ecosystem JSON files. Processes that already
exist (matched by command + working directory) are skipped with a warning.

This is useful for merging exported configs without creating duplicates:

  gopm export all > gopm.process
  gopm import gopm.process
  gopm import app1.json app2.json`,
	Args: cobra.MinimumNArgs(1),
	Run:  runImport,
}

func runImport(cmd *cobra.Command, args []string) {
	// Load all files, warn on bad ones but continue with the rest.
	var allApps []config.AppConfig
	for _, path := range args {
		eco, err := config.LoadEcosystem(path)
		if err != nil {
			fmt.Printf("%s %s: %v\n", display.Red("WARN"), display.Bold(path), err)
			continue
		}
		allApps = append(allApps, eco.Apps...)
	}
	if len(allApps) == 0 {
		exitError("no valid apps found in any input file")
	}

	c, err := newClient()
	if err != nil {
		exitError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	// Get existing processes to detect duplicates.
	resp, err := c.Send(protocol.MethodList, nil)
	if err != nil {
		exitError(fmt.Sprintf("failed to list processes: %v", err))
	}
	if !resp.Success {
		exitError(resp.Error)
	}

	var existing []protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &existing); err != nil {
		exitError(fmt.Sprintf("failed to parse process list: %v", err))
	}

	// Build lookup: "cwd + command" → process name.
	type key struct{ cwd, command string }
	existingSet := make(map[key]string)
	for _, p := range existing {
		existingSet[key{p.Cwd, p.Command}] = p.Name
	}

	imported := 0
	skipped := 0
	for _, app := range allApps {
		// Resolve cwd for comparison — empty means current dir (same as daemon default).
		cwd := app.Cwd
		if cwd == "" {
			cwd, _ = filepath.Abs(".")
		}
		cmd := app.Command
		if !filepath.IsAbs(cmd) {
			if abs, err := filepath.Abs(cmd); err == nil {
				cmd = abs
			}
		}

		if name, exists := existingSet[key{cwd, cmd}]; exists {
			fmt.Printf("%s %s (matches existing %q: %s in %s)\n",
				display.Yellow("SKIP"), display.Bold(app.Name),
				name, cmd, cwd)
			skipped++
			continue
		}

		params := app.ToStartParams()
		resp, err := c.Send(protocol.MethodStart, params)
		if err != nil {
			fmt.Printf("%s %s: %v\n", display.Red("FAIL"), display.Bold(app.Name), err)
			continue
		}
		if !resp.Success {
			fmt.Printf("%s %s: %s\n", display.Red("FAIL"), display.Bold(app.Name), resp.Error)
			continue
		}

		var info protocol.ProcessInfo
		if err := json.Unmarshal(resp.Data, &info); err == nil {
			fmt.Printf("%s %s (PID: %s)\n",
				display.Green("OK"), display.Bold(info.Name),
				display.Cyan(fmt.Sprintf("%d", info.PID)))
		}
		imported++
	}

	fmt.Printf("\nImported %d/%d processes", imported, len(allApps))
	if skipped > 0 {
		fmt.Printf(" (%d skipped)", skipped)
	}
	fmt.Println()
}

package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var rebootCmd = &cobra.Command{
	Use:   "reboot",
	Short: "Restart the daemon (save, kill, resurrect)",
	Long: `Restart the daemon while preserving all managed processes.

This command saves the current process list, kills the daemon, starts
a fresh daemon, and resurrects all previously running processes.`,
	Args: cobra.NoArgs,
	Run:  runReboot,
}

func runReboot(cmd *cobra.Command, args []string) {
	// Step 1: Save current state
	c, err := client.New()
	if err != nil {
		outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}

	fmt.Printf("[1/3] %s process list...\n", display.Dim("Saving"))
	resp, err := c.Send(protocol.MethodSave, nil)
	if err != nil {
		outputError(fmt.Sprintf("failed to save: %v", err))
	}
	if !resp.Success {
		outputError(resp.Error)
	}

	// Step 2: Kill daemon
	fmt.Printf("[2/3] %s daemon...\n", display.Dim("Stopping"))
	c.Send(protocol.MethodKill, nil)
	c.Close()

	// Wait for daemon to fully stop
	time.Sleep(500 * time.Millisecond)

	// Step 3: Start fresh daemon and resurrect
	fmt.Printf("[3/3] %s daemon and restoring processes...\n", display.Dim("Starting"))
	c2, err := client.New()
	if err != nil {
		outputError(fmt.Sprintf("failed to start new daemon: %v", err))
	}
	defer c2.Close()

	resp, err = c2.Send(protocol.MethodResurrect, nil)
	if err != nil {
		outputError(fmt.Sprintf("failed to resurrect: %v", err))
	}
	if !resp.Success {
		outputError(resp.Error)
	}

	if jsonOutput {
		outputJSON(resp.Data)
		return
	}

	// Get new daemon info
	time.Sleep(200 * time.Millisecond)
	pingResp, _ := c2.Send(protocol.MethodPing, nil)
	var ping protocol.PingResult
	if pingResp != nil && pingResp.Success {
		json.Unmarshal(pingResp.Data, &ping)
	}

	fmt.Printf("%s daemon rebooted (PID: %s)\n",
		display.Bold("gopm"), display.Cyan(fmt.Sprintf("%d", ping.PID)))
}

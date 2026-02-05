package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
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
	c.Close()

	// Read dump.json into memory BEFORE killing.
	// The daemon's shutdown() will overwrite it with stopped-status processes.
	dumpPath := protocol.DumpFilePath()
	savedDump, err := os.ReadFile(dumpPath)
	if err != nil {
		outputError(fmt.Sprintf("failed to read dump file: %v", err))
	}

	// Read daemon PID so we can wait for the process to truly exit.
	daemonPID := readDaemonPID()

	// Step 2: Kill daemon
	fmt.Printf("[2/3] %s daemon...\n", display.Dim("Stopping"))
	c2, err := client.New()
	if err != nil {
		outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	c2.Send(protocol.MethodKill, nil)
	c2.Close()

	// Wait for the daemon PROCESS to actually exit (not just socket close).
	// The daemon closes the listener first, then stops processes, saves state,
	// removes files, and finally exits. We must wait for the full exit.
	waitForProcessExit(daemonPID, 15*time.Second)

	// Restore the good dump.json (with online statuses).
	// Safe now because the daemon has fully exited.
	os.WriteFile(dumpPath, savedDump, 0644)

	// Step 3: Start fresh daemon (auto-loads dump.json)
	fmt.Printf("[3/3] %s daemon and restoring processes...\n", display.Dim("Starting"))
	c3, err := client.New()
	if err != nil {
		outputError(fmt.Sprintf("failed to start new daemon: %v", err))
	}
	defer c3.Close()

	if jsonOutput {
		resp, err := c3.Send(protocol.MethodPing, nil)
		if err != nil {
			outputError(fmt.Sprintf("failed to ping new daemon: %v", err))
		}
		outputJSON(resp.Data)
		return
	}

	// Ping new daemon for info
	pingResp, err := c3.Send(protocol.MethodPing, nil)
	var ping protocol.PingResult
	if err == nil && pingResp != nil && pingResp.Success {
		json.Unmarshal(pingResp.Data, &ping)
	}

	fmt.Printf("%s daemon rebooted (PID: %s)\n",
		display.Bold("gopm"), display.Cyan(fmt.Sprintf("%d", ping.PID)))
}

// readDaemonPID reads the daemon PID from the PID file.
func readDaemonPID() int {
	data, err := os.ReadFile(protocol.PIDFilePath())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// waitForProcessExit polls until a process no longer exists.
func waitForProcessExit(pid int, timeout time.Duration) {
	if pid == 0 {
		time.Sleep(1 * time.Second)
		return
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if err != nil {
			return // process is gone
		}
		time.Sleep(100 * time.Millisecond)
	}
}

package cli

import (
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
	Short: "Restart the daemon (save, stop, exit, resurrect)",
	Long: `Restart the daemon while preserving all managed processes.

The daemon saves the current process list, stops all processes, and exits.
If gopm is installed as a systemd service, systemd restarts the daemon
automatically (within ~5 seconds). Otherwise the CLI restarts it directly.
On startup the daemon resurrects all previously online processes.`,
	Args: cobra.NoArgs,
	Run:  runReboot,
}

func runReboot(cmd *cobra.Command, args []string) {
	c, err := client.NewWithConfig(configFlag)
	if err != nil {
		outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}

	// Read daemon PID so we can wait for the process to truly exit.
	daemonPID := readDaemonPID()

	// Send reboot — daemon saves state (online), then stops processes and exits.
	fmt.Printf("[1/2] %s and stopping processes...\n", display.Dim("Saving"))
	resp, err := c.Send(protocol.MethodReboot, nil)
	if err != nil {
		outputError(fmt.Sprintf("reboot failed: %v", err))
	}
	if !resp.Success {
		outputError(resp.Error)
	}
	c.Close()

	// Wait for old daemon to fully exit.
	waitForProcessExit(daemonPID, 15*time.Second)

	if isSystemdInstalled() {
		// Systemd will restart the daemon automatically.
		fmt.Printf("[2/2] Daemon exited — %s will restart it in ~5s\n", display.Bold("systemd"))
		if jsonOutput {
			fmt.Println(`{"status":"rebooting","restart":"systemd"}`)
		}
		return
	}

	// No systemd — restart the daemon ourselves.
	fmt.Printf("[2/2] %s daemon and restoring processes...\n", display.Dim("Starting"))
	c2, err := client.NewWithConfig(configFlag)
	if err != nil {
		outputError(fmt.Sprintf("failed to start new daemon: %v", err))
	}
	defer c2.Close()

	if jsonOutput {
		resp, err := c2.Send(protocol.MethodPing, nil)
		if err != nil {
			outputError(fmt.Sprintf("failed to ping new daemon: %v", err))
		}
		outputJSON(resp.Data)
		return
	}

	fmt.Printf("%s daemon rebooted\n", display.Bold("gopm"))
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

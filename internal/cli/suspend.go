package cli

import (
	"fmt"
	"os/exec"

	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var suspendCmd = &cobra.Command{
	Use:   "suspend",
	Short: "Save state, stop daemon, and disable the systemd service",
	Long: `Suspend gopm: save the process list, stop the daemon, and disable the
systemd service so it does not restart automatically.

Use "gopm unsuspend" to re-enable the service and start everything back.`,
	Args: cobra.NoArgs,
	Run:  runSuspend,
}

var unsuspendCmd = &cobra.Command{
	Use:   "unsuspend",
	Short: "Enable the systemd service and start the daemon",
	Long: `Re-enable the gopm systemd service and start it. The daemon will
automatically resurrect all processes that were online when suspended.`,
	Args: cobra.NoArgs,
	Run:  runUnsuspend,
}

func runSuspend(cmd *cobra.Command, args []string) {
	if !isSystemdInstalled() {
		outputError("gopm is not installed as a systemd service (run: sudo gopm install)")
	}

	// Save state while processes are still online.
	c, err := tryClient()
	if err == nil {
		fmt.Printf("[1/3] %s process list...\n", display.Dim("Saving"))
		resp, err := c.Send(protocol.MethodSave, nil)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: save failed: %v\n", err)
		} else if !resp.Success {
			fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: save failed: %s\n", resp.Error)
		}
		c.Close()
	} else {
		fmt.Printf("[1/3] %s\n", display.Dim("Daemon not running, skipping save"))
	}

	// Stop the systemd service (this also stops the daemon and all processes).
	fmt.Printf("[2/3] %s gopm service...\n", display.Dim("Stopping"))
	if out, err := exec.Command("systemctl", "stop", "gopm").CombinedOutput(); err != nil {
		outputError(fmt.Sprintf("systemctl stop gopm failed: %v\n%s", err, out))
	}

	// Disable the service so it doesn't start on boot or get auto-restarted.
	fmt.Printf("[3/3] %s gopm service...\n", display.Dim("Disabling"))
	if out, err := exec.Command("systemctl", "disable", "gopm").CombinedOutput(); err != nil {
		outputError(fmt.Sprintf("systemctl disable gopm failed: %v\n%s", err, out))
	}

	fmt.Printf("%s suspended — use %s to resume\n",
		display.Bold("gopm"), display.Cyan("gopm unsuspend"))
}

func runUnsuspend(cmd *cobra.Command, args []string) {
	if !isSystemdInstalled() {
		outputError("gopm is not installed as a systemd service (run: sudo gopm install)")
	}

	// Re-enable the service.
	fmt.Printf("[1/2] %s gopm service...\n", display.Dim("Enabling"))
	if out, err := exec.Command("systemctl", "enable", "gopm").CombinedOutput(); err != nil {
		outputError(fmt.Sprintf("systemctl enable gopm failed: %v\n%s", err, out))
	}

	// Start the service (daemon auto-resurrects from dump.json).
	fmt.Printf("[2/2] %s gopm service...\n", display.Dim("Starting"))
	if out, err := exec.Command("systemctl", "start", "gopm").CombinedOutput(); err != nil {
		outputError(fmt.Sprintf("systemctl start gopm failed: %v\n%s", err, out))
	}

	fmt.Printf("%s resumed — processes are being restored\n", display.Bold("gopm"))
}

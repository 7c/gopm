package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"text/template"

	"github.com/spf13/cobra"
)

const unitFilePath = "/etc/systemd/system/gopm.service"

var unitTemplate = template.Must(template.New("unit").Parse(`[Unit]
Description=GoPM Process Manager
After=network.target

[Service]
Type=forking
User={{.User}}
Environment=HOME={{.Home}}
PIDFile={{.Home}}/.gopm/daemon.pid
ExecStart=/usr/local/bin/gopm resurrect
ExecStop=/usr/local/bin/gopm kill
ExecReload=/usr/local/bin/gopm restart all
Restart=always
RestartSec=5
LimitNOFILE=65536
LimitNPROC=65536

[Install]
WantedBy=multi-user.target
`))

type unitData struct {
	User string
	Home string
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install gopm as a systemd service",
	Args:  cobra.NoArgs,
	Run:   runInstall,
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove gopm systemd service",
	Args:  cobra.NoArgs,
	Run:   runUninstall,
}

var installUser string

func init() {
	installCmd.Flags().StringVar(&installUser, "user", "", "user to run the service as")
}

func runInstall(cmd *cobra.Command, args []string) {
	if os.Getuid() != 0 {
		outputError("this command must be run as root (use sudo)")
	}

	// Resolve the target user.
	username := installUser
	if username == "" {
		username = os.Getenv("SUDO_USER")
	}
	if username == "" {
		u, err := user.Current()
		if err != nil {
			outputError(fmt.Sprintf("cannot determine current user: %v", err))
		}
		username = u.Username
	}

	u, err := user.Lookup(username)
	if err != nil {
		outputError(fmt.Sprintf("cannot look up user %q: %v", username, err))
	}

	data := unitData{
		User: u.Username,
		Home: u.HomeDir,
	}

	// [1/5] Symlink binary to /usr/local/bin/gopm.
	fmt.Println("[1/5] Linking binary to /usr/local/bin/gopm...")
	self, err := os.Executable()
	if err != nil {
		outputError(fmt.Sprintf("cannot find gopm binary: %v", err))
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		outputError(fmt.Sprintf("cannot resolve binary path: %v", err))
	}
	self, _ = filepath.Abs(self)

	const installPath = "/usr/local/bin/gopm"
	if self == installPath {
		fmt.Println("  binary already at /usr/local/bin/gopm, skipping")
	} else {
		// Remove existing file or symlink before creating a new one.
		os.Remove(installPath)
		if err := os.Symlink(self, installPath); err != nil {
			outputError(fmt.Sprintf("cannot create symlink: %v", err))
		}
		fmt.Printf("  %s → %s\n", installPath, self)
	}

	// [2/5] Write systemd unit file.
	fmt.Println("[2/5] Writing systemd unit file...")
	f, err := os.Create(unitFilePath)
	if err != nil {
		outputError(fmt.Sprintf("cannot create unit file: %v", err))
	}
	if err := unitTemplate.Execute(f, data); err != nil {
		f.Close()
		outputError(fmt.Sprintf("cannot write unit file: %v", err))
	}
	f.Close()

	// [3/5] Reload systemd daemon.
	fmt.Println("[3/5] Reloading systemd daemon...")
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		outputError(fmt.Sprintf("daemon-reload failed: %v\n%s", err, out))
	}

	// [4/5] Enable the service.
	fmt.Println("[4/5] Enabling gopm service...")
	if out, err := exec.Command("systemctl", "enable", "gopm").CombinedOutput(); err != nil {
		outputError(fmt.Sprintf("enable failed: %v\n%s", err, out))
	}

	// [5/5] Start the service.
	fmt.Println("[5/5] Starting gopm service...")
	if out, err := exec.Command("systemctl", "start", "gopm").CombinedOutput(); err != nil {
		outputError(fmt.Sprintf("start failed: %v\n%s", err, out))
	}

	fmt.Printf("gopm installed and running as systemd service (user: %s)\n", u.Username)
}

func runUninstall(cmd *cobra.Command, args []string) {
	if os.Getuid() != 0 {
		outputError("this command must be run as root (use sudo)")
	}

	// [1/5] Stop the service.
	fmt.Println("[1/5] Stopping gopm service...")
	// Ignore errors — the service may not be running.
	exec.Command("systemctl", "stop", "gopm").CombinedOutput()

	// [2/5] Disable the service.
	fmt.Println("[2/5] Disabling gopm service...")
	exec.Command("systemctl", "disable", "gopm").CombinedOutput()

	// [3/5] Remove unit file.
	fmt.Println("[3/5] Removing unit file...")
	if err := os.Remove(unitFilePath); err != nil && !os.IsNotExist(err) {
		outputError(fmt.Sprintf("cannot remove unit file: %v", err))
	}

	// [4/5] Reload systemd daemon.
	fmt.Println("[4/5] Reloading systemd daemon...")
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		outputError(fmt.Sprintf("daemon-reload failed: %v\n%s", err, out))
	}

	// [5/5] Remove symlink from /usr/local/bin.
	fmt.Println("[5/5] Removing /usr/local/bin/gopm...")
	if err := os.Remove("/usr/local/bin/gopm"); err != nil && !os.IsNotExist(err) {
		outputError(fmt.Sprintf("cannot remove symlink: %v", err))
	}

	fmt.Println("gopm systemd service removed")
}

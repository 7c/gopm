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

	// [1/5] Copy binary to /usr/local/bin/gopm.
	fmt.Println("[1/5] Copying binary to /usr/local/bin/gopm...")
	self, err := os.Executable()
	if err != nil {
		outputError(fmt.Sprintf("cannot find gopm binary: %v", err))
	}
	self, _ = filepath.EvalSymlinks(self)

	input, err := os.ReadFile(self)
	if err != nil {
		outputError(fmt.Sprintf("cannot read binary: %v", err))
	}
	if err := os.WriteFile("/usr/local/bin/gopm", input, 0755); err != nil {
		outputError(fmt.Sprintf("cannot write binary: %v", err))
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

	// [1/4] Stop the service.
	fmt.Println("[1/4] Stopping gopm service...")
	// Ignore errors — the service may not be running.
	exec.Command("systemctl", "stop", "gopm").CombinedOutput()

	// [2/4] Disable the service.
	fmt.Println("[2/4] Disabling gopm service...")
	exec.Command("systemctl", "disable", "gopm").CombinedOutput()

	// [3/4] Remove unit file.
	fmt.Println("[3/4] Removing unit file...")
	if err := os.Remove(unitFilePath); err != nil && !os.IsNotExist(err) {
		outputError(fmt.Sprintf("cannot remove unit file: %v", err))
	}

	// [4/4] Reload systemd daemon.
	fmt.Println("[4/4] Reloading systemd daemon...")
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		outputError(fmt.Sprintf("daemon-reload failed: %v\n%s", err, out))
	}

	fmt.Println("gopm systemd service removed")
}

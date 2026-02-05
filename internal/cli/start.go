package cli

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/config"
	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start <script|binary|config.json> [flags] [-- args...]",
	Short: "Start a process or load an ecosystem config",
	Long: `Start a process, a script via an interpreter, or all apps from an ecosystem JSON file.

The first argument is a command/binary path, a script path (with --interpreter),
or a .json ecosystem config file. Everything after "--" is passed as arguments
to the child process.`,
	Example: `  # Start a Node.js application
  gopm start app.js --interpreter node --name my-api
  gopm start node --name my-api -- server.js --port 3000
  gopm start node --name my-api --cwd /srv/app --env NODE_ENV=production -- index.js

  # Start a Go binary
  gopm start ./myserver --name backend -- --listen :8080
  gopm start /usr/local/bin/myserver --name backend --autorestart on-failure

  # Start a bash script
  gopm start script.sh --interpreter bash --name worker
  gopm start bash --name cron-job -- -c "while true; do ./sync.sh; sleep 60; done"

  # Start with restart policies
  gopm start ./worker --name worker --autorestart always --max-restarts 10
  gopm start ./task --name task --autorestart on-failure --restart-delay 5s --exp-backoff
  gopm start ./job --name job --autorestart never

  # Start with custom log settings
  gopm start ./app --name app --log-out /var/log/app.log --max-log-size 50M

  # Start all apps from an ecosystem config
  gopm start ecosystem.json`,
	Args: cobra.MinimumNArgs(1),
	// TraverseChildren allows flags after positional args and before "--".
	TraverseChildren: true,
	Run:              runStart,
}

// start-specific flags
var (
	startName        string
	startCwd         string
	startInterpreter string
	startEnv         []string
	startAutoRestart string
	startMaxRestarts int
	startMinUptime   string
	startRestartDelay string
	startExpBackoff  bool
	startMaxDelay    string
	startKillTimeout string
	startLogOut      string
	startLogErr      string
	startMaxLogSize  string
)

func init() {
	f := startCmd.Flags()
	f.StringVar(&startName, "name", "", "process name")
	f.StringVar(&startCwd, "cwd", "", "working directory")
	f.StringVar(&startInterpreter, "interpreter", "", "interpreter (e.g. node, python3)")
	f.StringArrayVar(&startEnv, "env", nil, "environment variable KEY=VAL (repeatable)")
	f.StringVar(&startAutoRestart, "autorestart", "", "restart policy: always|on-failure|never")
	f.IntVar(&startMaxRestarts, "max-restarts", -1, "max restart attempts (-1 = use default)")
	f.StringVar(&startMinUptime, "min-uptime", "", "minimum uptime before considered stable (e.g. 5s)")
	f.StringVar(&startRestartDelay, "restart-delay", "", "delay between restarts (e.g. 1s)")
	f.BoolVar(&startExpBackoff, "exp-backoff", false, "enable exponential backoff on restarts")
	f.StringVar(&startMaxDelay, "max-delay", "", "max delay for exponential backoff (e.g. 30s)")
	f.StringVar(&startKillTimeout, "kill-timeout", "", "time to wait for graceful stop (e.g. 5s)")
	f.StringVar(&startLogOut, "log-out", "", "stdout log file path")
	f.StringVar(&startLogErr, "log-err", "", "stderr log file path")
	f.StringVar(&startMaxLogSize, "max-log-size", "", "max log file size before rotation (e.g. 10M)")
}

func runStart(cmd *cobra.Command, args []string) {
	target := args[0]

	// Collect everything after "--" as child process arguments.
	childArgs := args[1:]

	// If the target is a .json file, treat it as an ecosystem config.
	if strings.HasSuffix(target, ".json") {
		startEcosystem(target)
		return
	}

	startSingle(target, childArgs)
}

// startEcosystem loads an ecosystem JSON file and starts each app.
func startEcosystem(path string) {
	eco, err := config.LoadEcosystem(path)
	if err != nil {
		exitError(fmt.Sprintf("failed to load ecosystem config: %v", err))
	}

	c, err := client.NewWithConfig(configFlag)
	if err != nil {
		exitError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	for _, app := range eco.Apps {
		params := app.ToStartParams()
		resp, err := c.Send(protocol.MethodStart, params)
		if err != nil {
			exitError(fmt.Sprintf("failed to start %q: %v", app.Name, err))
		}
		if !resp.Success {
			exitError(fmt.Sprintf("failed to start %q: %s", app.Name, resp.Error))
		}

		if jsonOutput {
			fmt.Println(string(resp.Data))
		} else {
			var info protocol.ProcessInfo
			if err := json.Unmarshal(resp.Data, &info); err == nil {
				fmt.Printf("Process %s %s (PID: %s)\n", display.Bold(info.Name), display.Green("started"), display.Cyan(fmt.Sprintf("%d", info.PID)))
			}
		}
	}
}

// startSingle starts a single process from CLI flags.
func startSingle(command string, childArgs []string) {
	// Resolve command to absolute path.
	if !filepath.IsAbs(command) {
		if strings.Contains(command, "/") {
			// Relative path with directory component - resolve from CWD.
			if abs, err := filepath.Abs(command); err == nil {
				command = abs
			}
		} else {
			// Bare command name - look up in PATH.
			if abs, err := exec.LookPath(command); err == nil {
				command = abs
			}
		}
	}

	params := protocol.StartParams{
		Command:      command,
		Name:         startName,
		Args:         childArgs,
		Cwd:          startCwd,
		Interpreter:  startInterpreter,
		AutoRestart:  startAutoRestart,
		ExpBackoff:   startExpBackoff,
		MinUptime:    startMinUptime,
		RestartDelay: startRestartDelay,
		MaxDelay:     startMaxDelay,
		KillTimeout:  startKillTimeout,
		LogOut:       startLogOut,
		LogErr:       startLogErr,
		MaxLogSize:   startMaxLogSize,
	}

	if startMaxRestarts >= 0 {
		params.MaxRestarts = &startMaxRestarts
	}

	// Parse --env KEY=VAL entries into a map.
	if len(startEnv) > 0 {
		envMap := make(map[string]string, len(startEnv))
		for _, entry := range startEnv {
			k, v, ok := strings.Cut(entry, "=")
			if !ok {
				exitError(fmt.Sprintf("invalid --env value %q: expected KEY=VAL", entry))
			}
			envMap[k] = v
		}
		params.Env = envMap
	}

	c, err := client.NewWithConfig(configFlag)
	if err != nil {
		exitError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	resp, err := c.Send(protocol.MethodStart, params)
	if err != nil {
		exitError(fmt.Sprintf("failed to start process: %v", err))
	}
	if !resp.Success {
		exitError(resp.Error)
	}

	if jsonOutput {
		fmt.Println(string(resp.Data))
	} else {
		var info protocol.ProcessInfo
		if err := json.Unmarshal(resp.Data, &info); err == nil {
			fmt.Printf("Process %s %s (PID: %s)\n", display.Bold(info.Name), display.Green("started"), display.Cyan(fmt.Sprintf("%d", info.PID)))
		}
	}
}

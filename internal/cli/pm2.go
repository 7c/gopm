package cli

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var pm2Cmd = &cobra.Command{
	Use:   "pm2",
	Short: "Import all processes from PM2 into gopm",
	Long: `Read the PM2 process list, start each process in gopm, and remove it from PM2.

This is a one-time migration command. For each PM2 process it:
  1. Reads the process configuration (script, args, env, restart policy, etc.)
  2. Starts the process in gopm with equivalent settings
  3. Deletes the process from PM2

Verbose output is printed for every step so you can verify the migration.
Cluster-mode processes are imported as single fork-mode processes.`,
	Args: cobra.NoArgs,
	Run:  runPM2Import,
}

// pm2 jlist JSON structures

type pm2Process struct {
	Name   string `json:"name"`
	PM2ID  int    `json:"pm_id"`
	PID    int    `json:"pid"`
	PM2Env pm2Env `json:"pm2_env"`
}

type pm2Env struct {
	Status       string                 `json:"status"`
	ExecPath     string                 `json:"pm_exec_path"`
	Cwd          string                 `json:"pm_cwd"`
	Args         json.RawMessage        `json:"args"`
	Interpreter  string                 `json:"exec_interpreter"`
	Env          map[string]interface{} `json:"env"`
	AutoRestart  interface{}            `json:"autorestart"`
	MaxRestarts  int                    `json:"max_restarts"`
	MinUptime    interface{}            `json:"min_uptime"`
	RestartDelay int                    `json:"restart_delay"`
	KillTimeout  int                    `json:"kill_timeout"`
	ExecMode     string                 `json:"exec_mode"`
	Instances    int                    `json:"instances"`
}

// pm2InternalEnvKeys are env vars injected by PM2 that should not be imported.
var pm2InternalEnvKeys = map[string]bool{
	"status": true, "unique_id": true, "instances": true,
	"exec_mode": true, "vizion": true, "merge_logs": true,
	"axm_actions": true, "axm_monitor": true, "axm_options": true,
	"axm_dynamic": true, "automation": true, "name": true,
	"node_args": true, "unstable_restarts": true, "treekill": true,
	"exit_code": true, "prev_restart_delay": true, "version": true,
	"versioning": true, "restart_time": true, "created_at": true,
	"watch": true, "filter_env": true, "namespace": true,
	"kill_retry_time": true, "username": true, "windowsHide": true,
	"instance_var": true,
}

func runPM2Import(cmd *cobra.Command, args []string) {
	// Check pm2 exists.
	pm2Bin, err := exec.LookPath("pm2")
	if err != nil {
		outputError("pm2 not found in PATH")
	}
	fmt.Printf("Using %s\n\n", display.Dim(pm2Bin))

	// Get PM2 process list.
	out, err := exec.Command(pm2Bin, "jlist").Output()
	if err != nil {
		outputError(fmt.Sprintf("pm2 jlist failed: %v", err))
	}

	var procs []pm2Process
	if err := json.Unmarshal(out, &procs); err != nil {
		outputError(fmt.Sprintf("cannot parse pm2 jlist output: %v", err))
	}

	if len(procs) == 0 {
		fmt.Println("No PM2 processes found.")
		return
	}

	fmt.Printf("Found %s PM2 process(es)\n", display.Bold(fmt.Sprintf("%d", len(procs))))

	imported := 0
	for i, p := range procs {
		fmt.Printf("\n%s [%d/%d] %s (pm2_id=%d, PID=%d, %s) %s\n",
			display.Dim("━━━"),
			i+1, len(procs),
			display.Bold(p.Name),
			p.PM2ID, p.PID,
			pm2StatusColor(p.PM2Env.Status),
			display.Dim("━━━"),
		)

		params := pm2ToStartParams(p)
		printPM2Details(params, p)

		// Warn about cluster mode.
		if p.PM2Env.ExecMode == "cluster_mode" || p.PM2Env.Instances > 1 {
			fmt.Printf("  %s cluster mode with %d instance(s) — importing as single fork process\n",
				display.Yellow("!"), p.PM2Env.Instances)
		}

		// Start in gopm (new connection per request).
		c, err := client.NewWithConfig(configFlag)
		if err != nil {
			fmt.Printf("  %s connect to daemon: %v\n", display.Red("FAIL"), err)
			continue
		}

		fmt.Printf("  %s Starting in gopm... ", display.Dim("→"))
		resp, err := c.Send(protocol.MethodStart, params)
		c.Close()
		if err != nil {
			fmt.Printf("%s %v\n", display.Red("FAIL"), err)
			continue
		}
		if !resp.Success {
			fmt.Printf("%s %s\n", display.Red("FAIL"), resp.Error)
			continue
		}

		var info protocol.ProcessInfo
		if err := json.Unmarshal(resp.Data, &info); err == nil {
			fmt.Printf("%s (id=%d, PID=%d)\n", display.Green("OK"), info.ID, info.PID)
		} else {
			fmt.Printf("%s\n", display.Green("OK"))
		}

		// Remove from PM2.
		fmt.Printf("  %s Removing from PM2... ", display.Dim("→"))
		delOut, err := exec.Command(pm2Bin, "delete", p.Name).CombinedOutput()
		if err != nil {
			fmt.Printf("%s %v\n%s", display.Red("FAIL"), err, display.Dim(string(delOut)))
			// Process is already running in gopm, count it as imported.
		} else {
			fmt.Printf("%s\n", display.Green("OK"))
		}

		imported++
	}

	fmt.Printf("\nSummary: imported %s/%d processes\n",
		display.Bold(fmt.Sprintf("%d", imported)), len(procs))
}

// pm2ToStartParams converts a PM2 process to gopm StartParams.
func pm2ToStartParams(p pm2Process) protocol.StartParams {
	env := p.PM2Env

	params := protocol.StartParams{
		Command: env.ExecPath,
		Name:    p.Name,
		Cwd:     env.Cwd,
		Args:    parsePM2Args(env.Args),
	}

	// Interpreter: skip "none" (binary) and "node" when script IS the node binary.
	interp := env.Interpreter
	if interp != "" && interp != "none" {
		params.Interpreter = interp
	}

	// Environment: filter out PM2 internal vars.
	if len(env.Env) > 0 {
		filtered := make(map[string]string)
		for k, v := range env.Env {
			if isPM2InternalEnv(k) {
				continue
			}
			filtered[k] = fmt.Sprintf("%v", v)
		}
		if len(filtered) > 0 {
			params.Env = filtered
		}
	}

	// AutoRestart: PM2 uses bool, gopm uses string.
	if ar, ok := env.AutoRestart.(bool); ok {
		if ar {
			params.AutoRestart = "always"
		} else {
			params.AutoRestart = "never"
		}
	}

	// MaxRestarts.
	if env.MaxRestarts > 0 {
		mr := env.MaxRestarts
		params.MaxRestarts = &mr
	}

	// Duration fields: PM2 uses milliseconds (can be number or string).
	if ms := parsePM2Millis(env.MinUptime); ms > 0 {
		params.MinUptime = fmt.Sprintf("%dms", ms)
	}
	if env.RestartDelay > 0 {
		params.RestartDelay = fmt.Sprintf("%dms", env.RestartDelay)
	}
	if env.KillTimeout > 0 {
		params.KillTimeout = fmt.Sprintf("%dms", env.KillTimeout)
	}

	return params
}

// printPM2Details prints verbose details about the mapped process.
func printPM2Details(params protocol.StartParams, p pm2Process) {
	fmt.Printf("  %-14s %s\n", display.Dim("command:"), params.Command)
	if params.Interpreter != "" {
		fmt.Printf("  %-14s %s\n", display.Dim("interpreter:"), params.Interpreter)
	}
	if params.Cwd != "" {
		fmt.Printf("  %-14s %s\n", display.Dim("cwd:"), params.Cwd)
	}
	if len(params.Args) > 0 {
		fmt.Printf("  %-14s %s\n", display.Dim("args:"), strings.Join(params.Args, " "))
	}
	if len(params.Env) > 0 {
		keys := make([]string, 0, len(params.Env))
		for k := range params.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, len(keys))
		for i, k := range keys {
			pairs[i] = k + "=" + params.Env[k]
		}
		fmt.Printf("  %-14s %s\n", display.Dim("env:"), strings.Join(pairs, ", "))
	}
	if params.AutoRestart != "" {
		fmt.Printf("  %-14s %s\n", display.Dim("autorestart:"), params.AutoRestart)
	}
	if params.MaxRestarts != nil {
		fmt.Printf("  %-14s %d\n", display.Dim("max_restarts:"), *params.MaxRestarts)
	}
	if params.MinUptime != "" {
		fmt.Printf("  %-14s %s\n", display.Dim("min_uptime:"), params.MinUptime)
	}
	if params.RestartDelay != "" {
		fmt.Printf("  %-14s %s\n", display.Dim("restart_delay:"), params.RestartDelay)
	}
	if params.KillTimeout != "" {
		fmt.Printf("  %-14s %s\n", display.Dim("kill_timeout:"), params.KillTimeout)
	}
}

// parsePM2Args parses the pm2 args field which can be []string or a single string.
func parsePM2Args(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	// Try array first.
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	// Try single string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return strings.Fields(s)
	}
	return nil
}

// parsePM2Millis extracts a millisecond value from PM2's min_uptime
// which can be a number or a string like "1000".
func parsePM2Millis(v interface{}) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case string:
		var n int
		if _, err := fmt.Sscanf(val, "%d", &n); err == nil {
			return n
		}
	}
	return 0
}

// isPM2InternalEnv returns true if the env key is a PM2-internal variable.
func isPM2InternalEnv(key string) bool {
	if strings.HasPrefix(key, "pm_") || strings.HasPrefix(key, "PM2_") {
		return true
	}
	return pm2InternalEnvKeys[key]
}

// pm2StatusColor colors a PM2 status string.
func pm2StatusColor(status string) string {
	switch status {
	case "online":
		return display.Green(status)
	case "stopped", "stopping":
		return display.Yellow(status)
	case "errored":
		return display.Red(status)
	default:
		return status
	}
}

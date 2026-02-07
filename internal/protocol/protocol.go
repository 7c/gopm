package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// GopmHome returns the gopm state directory, respecting GOPM_HOME env var.
func GopmHome() string {
	if h := os.Getenv("GOPM_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gopm")
}

func SocketPath() string  { return filepath.Join(GopmHome(), "gopm.sock") }
func PIDFilePath() string { return filepath.Join(GopmHome(), "daemon.pid") }
func DumpFilePath() string { return filepath.Join(GopmHome(), "dump.json") }
func LogDir() string      { return filepath.Join(GopmHome(), "logs") }

// Method constants
const (
	MethodStart     = "start"
	MethodStop      = "stop"
	MethodRestart   = "restart"
	MethodDelete    = "delete"
	MethodList      = "list"
	MethodDescribe  = "describe"
	MethodIsRunning = "isrunning"
	MethodLogs      = "logs"
	MethodFlush     = "flush"
	MethodSave      = "save"
	MethodResurrect = "resurrect"
	MethodPing      = "ping"
	MethodKill      = "kill"
	MethodReboot    = "reboot"
)

// Request is the IPC message from CLI to daemon.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Response is the IPC message from daemon to CLI.
type Response struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// Status represents the lifecycle state of a managed process.
type Status string

const (
	StatusOnline  Status = "online"
	StatusStopped Status = "stopped"
	StatusErrored Status = "errored"
)

// AutoRestartMode controls when a process is automatically restarted.
type AutoRestartMode string

const (
	RestartAlways    AutoRestartMode = "always"
	RestartOnFailure AutoRestartMode = "on-failure"
	RestartNever     AutoRestartMode = "never"
)

// RestartPolicy configures restart behavior for a managed process.
type RestartPolicy struct {
	AutoRestart     AutoRestartMode `json:"autorestart"`
	MaxRestarts     int             `json:"max_restarts"`
	MinUptime       Duration        `json:"min_uptime"`
	RestartDelay    Duration        `json:"restart_delay"`
	ExpBackoff      bool            `json:"exp_backoff"`
	MaxDelay        Duration        `json:"max_delay"`
	RestartOnExit   []int           `json:"restart_on_exit,omitempty"`
	NoRestartOnExit []int           `json:"no_restart_on_exit,omitempty"`
	KillSignal      int             `json:"kill_signal"`
	KillTimeout     Duration        `json:"kill_timeout"`
}

// DefaultRestartPolicy returns the default restart policy.
func DefaultRestartPolicy() RestartPolicy {
	return RestartPolicy{
		AutoRestart:  RestartAlways,
		MaxRestarts:  0, // 0 = unlimited
		MinUptime:    Duration{5 * time.Second},
		RestartDelay: Duration{2 * time.Second},
		MaxDelay:     Duration{30 * time.Second},
		KillSignal:   15, // SIGTERM
		KillTimeout:  Duration{5 * time.Second},
	}
}

// ProcessInfo is the public representation of a managed process, sent over IPC.
type ProcessInfo struct {
	ID            int               `json:"id"`
	Name          string            `json:"name"`
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	Cwd           string            `json:"cwd"`
	Env           map[string]string `json:"env"`
	Interpreter   string            `json:"interpreter,omitempty"`
	Status        Status            `json:"status"`
	StatusReason  string            `json:"status_reason,omitempty"`
	PID           int               `json:"pid"`
	RestartPolicy RestartPolicy     `json:"restart_policy"`
	Restarts      int               `json:"restarts"`
	Uptime        time.Time         `json:"uptime"`
	CreatedAt     time.Time         `json:"created_at"`
	ExitCode      int               `json:"exit_code"`
	Memory        uint64            `json:"memory"`
	CPU           float64           `json:"cpu"`
	Listeners     []string          `json:"listeners"`
	LogOut        string            `json:"log_out"`
	LogErr        string            `json:"log_err"`
	MaxLogSize    int64             `json:"max_log_size"`
}

// StartParams are the parameters for the "start" method.
type StartParams struct {
	Command      string            `json:"command"`
	Name         string            `json:"name,omitempty"`
	Args         []string          `json:"args,omitempty"`
	Cwd          string            `json:"cwd,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Interpreter  string            `json:"interpreter,omitempty"`
	AutoRestart  string            `json:"autorestart,omitempty"`
	MaxRestarts  *int              `json:"max_restarts,omitempty"`
	MinUptime    string            `json:"min_uptime,omitempty"`
	RestartDelay string            `json:"restart_delay,omitempty"`
	ExpBackoff   bool              `json:"exp_backoff,omitempty"`
	MaxDelay     string            `json:"max_delay,omitempty"`
	KillTimeout  string            `json:"kill_timeout,omitempty"`
	LogOut       string            `json:"log_out,omitempty"`
	LogErr       string            `json:"log_err,omitempty"`
	MaxLogSize   string            `json:"max_log_size,omitempty"`
}

// TargetParams identifies a process by name, ID, or "all".
type TargetParams struct {
	Target string `json:"target"`
}

// LogsParams are the parameters for the "logs" method.
type LogsParams struct {
	Target  string `json:"target"`
	Lines   int    `json:"lines"`
	ErrOnly bool   `json:"err_only"`
}

// PingResult is returned by the "ping" method.
type PingResult struct {
	PID          int    `json:"pid"`
	Uptime       string `json:"uptime"`
	UptimeMs     int64  `json:"uptime_ms"`
	Version      string `json:"version"`
	ConfigFile   string `json:"config_file,omitempty"`
	ConfigSource string `json:"config_source,omitempty"`
}

// IsRunningResult is returned by the "isrunning" method.
type IsRunningResult struct {
	Name     string `json:"name"`
	Running  bool   `json:"running"`
	Status   Status `json:"status"`
	PID      int    `json:"pid"`
	Uptime   string `json:"uptime,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Restarts int    `json:"restarts,omitempty"`
}

// Duration wraps time.Duration with JSON string marshaling (Go duration format).
type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch val := v.(type) {
	case float64:
		d.Duration = time.Duration(int64(val))
	case string:
		if val == "" {
			d.Duration = 0
			return nil
		}
		var err error
		d.Duration, err = time.ParseDuration(val)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid duration: %v", v)
	}
	return nil
}

// ParseSize parses a human-readable size string (e.g. "1M", "500K", "1G") to bytes.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 1048576, nil // default 1MB
	}
	var multiplier int64 = 1
	numStr := s
	switch {
	case strings.HasSuffix(s, "G"):
		multiplier = 1024 * 1024 * 1024
		numStr = s[:len(s)-1]
	case strings.HasSuffix(s, "M"):
		multiplier = 1024 * 1024
		numStr = s[:len(s)-1]
	case strings.HasSuffix(s, "K"):
		multiplier = 1024
		numStr = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	return n * multiplier, nil
}

// FormatSize converts bytes to a compact size string (e.g. "1M", "50M", "1G").
// This is the inverse of ParseSize.
func FormatSize(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB && b%GB == 0:
		return fmt.Sprintf("%dG", b/GB)
	case b >= MB && b%MB == 0:
		return fmt.Sprintf("%dM", b/MB)
	case b >= KB && b%KB == 0:
		return fmt.Sprintf("%dK", b/KB)
	default:
		return fmt.Sprintf("%d", b)
	}
}

// FormatDuration formats a duration in a human-friendly way.
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if len(parts) == 0 && seconds > 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}
	return strings.Join(parts, " ")
}

// FormatBytes formats bytes in a human-friendly way.
func FormatBytes(b uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

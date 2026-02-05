//go:build linux

package procinspect

import "time"

// ProcessInfo holds all inspection data for a process.
type ProcessInfo struct {
	PID       int               `json:"pid"`
	Identity  Identity          `json:"identity"`
	Resources Resources         `json:"resources"`
	Tree      []TreeNode        `json:"tree"`
	FDs       []FDInfo          `json:"fds"`
	Sockets   []SocketInfo      `json:"sockets"`
	Env       map[string]string `json:"env"`
	Cgroup    CgroupInfo        `json:"cgroup"`
	GoPM      *GoPMInfo         `json:"gopm"`
}

type Identity struct {
	Name       string    `json:"name"`
	State      string    `json:"state"`
	StateHuman string    `json:"state_human"`
	Cmdline    []string  `json:"cmdline"`
	Exe        string    `json:"exe"`
	ExeExists  bool      `json:"exe_exists"`
	CWD        string    `json:"cwd"`
	Root       string    `json:"root"`
	StartedAt  time.Time `json:"started_at"`
	StartedAgo string    `json:"started_ago"`
	UID        int       `json:"uid"`
	User       string    `json:"user"`
	GID        int       `json:"gid"`
	Group      string    `json:"group"`
	Session    int       `json:"session"`
	TTY        string    `json:"tty"`
	Nice       int       `json:"nice"`
	Threads    int       `json:"threads"`
}

type Resources struct {
	CPUUserSec     float64 `json:"cpu_user_seconds"`
	CPUSystemSec   float64 `json:"cpu_system_seconds"`
	CPUPercent     float64 `json:"cpu_percent"`
	VmPeak         int64   `json:"vm_peak_bytes"`
	VmRSS          int64   `json:"vm_rss_bytes"`
	VmSwap         int64   `json:"vm_swap_bytes"`
	VmSize         int64   `json:"vm_size_bytes"`
	Shared         int64   `json:"shared_bytes"`
	FDsOpen        int     `json:"fds_open"`
	FDsSoftLimit   int     `json:"fds_soft_limit"`
	FDsHardLimit   int     `json:"fds_hard_limit"`
	VoluntaryCSW   int64   `json:"voluntary_ctxt_switches"`
	InvoluntaryCSW int64   `json:"involuntary_ctxt_switches"`
}

type TreeNode struct {
	PID        int       `json:"pid"`
	PPid       int       `json:"ppid"`
	Exe        string    `json:"exe"`
	Cmdline    string    `json:"cmdline"`
	User       string    `json:"user"`
	StartedAt  time.Time `json:"started_at"`
	StartedAgo string    `json:"started_ago"`
}

type FDInfo struct {
	FD     int    `json:"fd"`
	Type   string `json:"type"`
	Mode   string `json:"mode"`
	Target string `json:"target"`
	Inode  int    `json:"inode,omitempty"`
}

type SocketInfo struct {
	Proto  string `json:"proto"`
	Local  string `json:"local"`
	Remote string `json:"remote"`
	State  string `json:"state"`
	FD     int    `json:"fd,omitempty"`
	Inode  int    `json:"inode"`
}

type CgroupInfo struct {
	Path       string `json:"path"`
	OOMScore   int    `json:"oom_score"`
	OOMAdj     int    `json:"oom_score_adj"`
	Seccomp    int    `json:"seccomp"`
	NoNewPrivs int    `json:"no_new_privs"`
	CapEff     string `json:"cap_eff"`
}

type GoPMInfo struct {
	Managed     bool   `json:"managed"`
	DaemonUp    bool   `json:"daemon_up"`
	Name        string `json:"name,omitempty"`
	ID          int    `json:"id,omitempty"`
	Restarts    int    `json:"restarts,omitempty"`
	AutoRestart string `json:"autorestart,omitempty"`
	LogOut      string `json:"log_out,omitempty"`
	LogErr      string `json:"log_err,omitempty"`
}

//go:build linux

package procinspect

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// FormatFull writes a complete formatted inspection to w.
func FormatFull(w io.Writer, info *ProcessInfo) {
	line := strings.Repeat("=", 70)
	fmt.Fprintf(w, "%s\n", line)
	fmt.Fprintf(w, "  Process Inspection — PID %d\n", info.PID)
	fmt.Fprintf(w, "%s\n\n", line)

	formatIdentity(w, &info.Identity)
	formatResources(w, &info.Resources)
	formatTree(w, info.Tree)
	formatFDs(w, info.FDs)
	formatSockets(w, info.Sockets)
	formatEnv(w, info.Env)
	formatCgroup(w, &info.Cgroup)
	if info.GoPM != nil {
		formatGoPM(w, info.GoPM)
	}

	fmt.Fprintf(w, "%s\n", line)
}

// FormatSections writes only the requested sections.
func FormatSections(w io.Writer, info *ProcessInfo, sections []string) {
	sectionSet := make(map[string]bool)
	for _, s := range sections {
		sectionSet[s] = true
	}

	// Always show identity one-liner
	fmt.Fprintf(w, "  PID %d  %s  %s (%s)\n\n", info.PID, info.Identity.Name,
		info.Identity.State, info.Identity.StateHuman)

	if sectionSet["tree"] {
		formatTree(w, info.Tree)
	}
	if sectionSet["fds"] {
		formatFDs(w, info.FDs)
	}
	if sectionSet["env"] {
		formatEnv(w, info.Env)
	}
	if sectionSet["net"] || sectionSet["sockets"] {
		formatSockets(w, info.Sockets)
	}
}

func sectionHeader(w io.Writer, title string) {
	padding := 64 - len(title)
	if padding < 0 {
		padding = 0
	}
	fmt.Fprintf(w, "  +- %s %s+\n", title, strings.Repeat("-", padding))
}

func sectionRow(w io.Writer, key, val string) {
	fmt.Fprintf(w, "  | %-17s %s\n", key, val)
}

func sectionFooter(w io.Writer) {
	fmt.Fprintf(w, "  +%s+\n\n", strings.Repeat("-", 68))
}

func formatIdentity(w io.Writer, id *Identity) {
	sectionHeader(w, "Identity")
	sectionRow(w, "Name", id.Name)
	sectionRow(w, "State", fmt.Sprintf("%s (%s)", id.State, id.StateHuman))
	if len(id.Cmdline) > 0 {
		sectionRow(w, "Command", strings.Join(id.Cmdline, " "))
	}
	sectionRow(w, "Exe", id.Exe)
	sectionRow(w, "CWD", id.CWD)
	sectionRow(w, "Root", id.Root)
	if !id.StartedAt.IsZero() {
		sectionRow(w, "Started", fmt.Sprintf("%s (%s ago)", id.StartedAt.Format("2006-01-02 15:04:05 MST"), id.StartedAgo))
	}
	sectionRow(w, "User", fmt.Sprintf("%s (uid=%d)", id.User, id.UID))
	sectionRow(w, "Group", fmt.Sprintf("%s (gid=%d)", id.Group, id.GID))
	sectionRow(w, "Session ID", fmt.Sprintf("%d", id.Session))
	sectionRow(w, "TTY", id.TTY)
	sectionRow(w, "Nice", fmt.Sprintf("%d", id.Nice))
	sectionRow(w, "Threads", fmt.Sprintf("%d", id.Threads))
	sectionFooter(w)
}

func formatResources(w io.Writer, r *Resources) {
	sectionHeader(w, "Resources")
	sectionRow(w, "CPU User", fmt.Sprintf("%.2fs", r.CPUUserSec))
	sectionRow(w, "CPU System", fmt.Sprintf("%.2fs", r.CPUSystemSec))
	sectionRow(w, "CPU %%", fmt.Sprintf("%.1f%%", r.CPUPercent))
	sectionRow(w, "VmPeak", formatBytes(r.VmPeak))
	sectionRow(w, "VmRSS", formatBytes(r.VmRSS))
	sectionRow(w, "VmSwap", formatBytes(r.VmSwap))
	sectionRow(w, "VmSize", fmt.Sprintf("%s (virtual)", formatBytes(r.VmSize)))
	sectionRow(w, "Shared", formatBytes(r.Shared))
	sectionRow(w, "FDs Open", fmt.Sprintf("%d / %d (soft) / %d (hard)", r.FDsOpen, r.FDsSoftLimit, r.FDsHardLimit))
	sectionRow(w, "Voluntary CSW", fmt.Sprintf("%d", r.VoluntaryCSW))
	sectionRow(w, "Involuntary CSW", fmt.Sprintf("%d", r.InvoluntaryCSW))
	sectionFooter(w)
}

func formatTree(w io.Writer, tree []TreeNode) {
	sectionHeader(w, "Process Tree (child -> ancestor)")
	for i, node := range tree {
		indent := strings.Repeat("  ", i)
		prefix := "+-"
		if i == 0 {
			prefix = ""
		}
		line := fmt.Sprintf("PID %-6d %s", node.PID, truncate(node.Cmdline, 40))
		sectionRow(w, "", fmt.Sprintf("%s%s %s  %s  %s", indent, prefix, line, node.User, node.StartedAgo))
	}
	sectionFooter(w)
}

func formatFDs(w io.Writer, fds []FDInfo) {
	sectionHeader(w, fmt.Sprintf("File Descriptors (%d open)", len(fds)))
	fmt.Fprintf(w, "  | %-5s %-8s %-5s %s\n", "FD", "Type", "Mode", "Target")
	for i, fd := range fds {
		if i >= 50 {
			fmt.Fprintf(w, "  | ...  (%d more)\n", len(fds)-50)
			break
		}
		fmt.Fprintf(w, "  | %-5d %-8s %-5s %s\n", fd.FD, fd.Type, fd.Mode, truncate(fd.Target, 50))
	}
	sectionFooter(w)
}

func formatSockets(w io.Writer, sockets []SocketInfo) {
	sectionHeader(w, "Network Sockets")
	fmt.Fprintf(w, "  | %-6s %-22s %-22s %-13s %s\n", "Proto", "Local", "Remote", "State", "FD")
	for _, s := range sockets {
		fd := "-"
		if s.FD > 0 {
			fd = fmt.Sprintf("%d", s.FD)
		}
		fmt.Fprintf(w, "  | %-6s %-22s %-22s %-13s %s\n", s.Proto, s.Local, s.Remote, s.State, fd)
	}
	sectionFooter(w)
}

func formatEnv(w io.Writer, env map[string]string) {
	sectionHeader(w, fmt.Sprintf("Environment (%d vars)", len(env)))
	// Sort keys
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	shown := 0
	for _, k := range keys {
		if shown >= 20 {
			fmt.Fprintf(w, "  | ...              (%d more)\n", len(keys)-20)
			break
		}
		fmt.Fprintf(w, "  | %-17s %s\n", k, truncate(env[k], 50))
		shown++
	}
	sectionFooter(w)
}

func formatCgroup(w io.Writer, c *CgroupInfo) {
	sectionHeader(w, "Cgroup & Limits")
	sectionRow(w, "Cgroup", truncate(c.Path, 50))
	sectionRow(w, "OOM Score", fmt.Sprintf("%d", c.OOMScore))
	sectionRow(w, "OOM Adj", fmt.Sprintf("%d", c.OOMAdj))
	sectionRow(w, "Seccomp", fmt.Sprintf("%d", c.Seccomp))
	sectionRow(w, "NoNewPrivs", fmt.Sprintf("%d", c.NoNewPrivs))
	sectionRow(w, "CapEff", c.CapEff)
	sectionFooter(w)
}

func formatGoPM(w io.Writer, g *GoPMInfo) {
	sectionHeader(w, "GoPM Info")
	if !g.DaemonUp {
		sectionRow(w, "Managed", "unknown (gopm daemon not running)")
	} else if !g.Managed {
		sectionRow(w, "Managed", "no (PID not found in gopm process table)")
	} else {
		sectionRow(w, "Managed", "yes")
		sectionRow(w, "GoPM Name", g.Name)
		sectionRow(w, "GoPM ID", fmt.Sprintf("%d", g.ID))
		sectionRow(w, "Restarts", fmt.Sprintf("%d", g.Restarts))
		sectionRow(w, "Auto Restart", g.AutoRestart)
		sectionRow(w, "Stdout Log", g.LogOut)
		sectionRow(w, "Stderr Log", g.LogErr)
	}
	sectionFooter(w)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// FormatRaw dumps key /proc files for the given PID (debug mode).
func FormatRaw(w io.Writer, pid int) {
	files := []string{"status", "stat", "cmdline", "limits", "cgroup", "oom_score", "oom_score_adj"}
	for _, f := range files {
		fmt.Fprintf(w, "=== /proc/%d/%s ===\n", pid, f)
		content := readProcFile(pid, f)
		if content == "" {
			fmt.Fprintln(w, "(empty or unreadable)")
		} else {
			fmt.Fprintf(w, "%s\n", content)
		}
		fmt.Fprintln(w)
	}
}

func formatBytes(b int64) string {
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

//go:build linux

package procinspect

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const clockTicksPerSec = 100 // standard on virtually all Linux systems

// Inspect reads /proc/<pid> and returns complete process info.
func Inspect(pid int) (*ProcessInfo, error) {
	procDir := fmt.Sprintf("/proc/%d", pid)
	if _, err := os.Stat(procDir); err != nil {
		return nil, fmt.Errorf("PID %d — no such process", pid)
	}

	info := &ProcessInfo{PID: pid}
	info.Identity = inspectIdentity(pid)
	info.Resources = inspectResources(pid)
	info.Tree = buildProcessTree(pid)
	info.FDs = inspectFDs(pid, 200)
	info.Sockets = inspectSockets(pid, info.FDs)
	info.Env = inspectEnviron(pid)
	info.Cgroup = inspectCgroup(pid)
	return info, nil
}

// InspectSections returns only the requested sections.
func InspectSections(pid int, sections []string) (*ProcessInfo, error) {
	procDir := fmt.Sprintf("/proc/%d", pid)
	if _, err := os.Stat(procDir); err != nil {
		return nil, fmt.Errorf("PID %d — no such process", pid)
	}

	info := &ProcessInfo{PID: pid}
	sectionSet := make(map[string]bool)
	for _, s := range sections {
		sectionSet[s] = true
	}

	// Always include identity
	info.Identity = inspectIdentity(pid)

	if len(sections) == 0 || sectionSet["resources"] {
		info.Resources = inspectResources(pid)
	}
	if len(sections) == 0 || sectionSet["tree"] {
		info.Tree = buildProcessTree(pid)
	}
	if len(sections) == 0 || sectionSet["fds"] {
		limit := 200
		if sectionSet["fds"] {
			limit = 10000
		}
		info.FDs = inspectFDs(pid, limit)
	}
	if len(sections) == 0 || sectionSet["sockets"] {
		if info.FDs == nil {
			info.FDs = inspectFDs(pid, 200)
		}
		info.Sockets = inspectSockets(pid, info.FDs)
	}
	if len(sections) == 0 || sectionSet["env"] {
		info.Env = inspectEnviron(pid)
	}
	if len(sections) == 0 || sectionSet["cgroup"] {
		info.Cgroup = inspectCgroup(pid)
	}
	return info, nil
}

func inspectIdentity(pid int) Identity {
	id := Identity{}

	// Parse /proc/<pid>/status
	status := readProcFile(pid, "status")
	for _, line := range strings.Split(status, "\n") {
		parts := strings.SplitN(line, ":\t", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], strings.TrimSpace(parts[1])
		switch key {
		case "Name":
			id.Name = val
		case "State":
			fields := strings.Fields(val)
			if len(fields) > 0 {
				id.State = fields[0]
				id.StateHuman = stateToHuman(id.State)
			}
		case "Threads":
			id.Threads, _ = strconv.Atoi(val)
		case "Uid":
			fields := strings.Fields(val)
			if len(fields) > 0 {
				id.UID, _ = strconv.Atoi(fields[0])
				if u, err := user.LookupId(fields[0]); err == nil {
					id.User = u.Username
				}
			}
		case "Gid":
			fields := strings.Fields(val)
			if len(fields) > 0 {
				id.GID, _ = strconv.Atoi(fields[0])
				if g, err := user.LookupGroupId(fields[0]); err == nil {
					id.Group = g.Name
				}
			}
		}
	}

	// Parse /proc/<pid>/cmdline
	cmdline := readProcFile(pid, "cmdline")
	if cmdline != "" {
		id.Cmdline = strings.Split(strings.TrimRight(cmdline, "\x00"), "\x00")
	}

	// Readlink /proc/<pid>/exe
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err == nil {
		id.Exe = exe
		id.ExeExists = !strings.HasSuffix(exe, " (deleted)")
	}

	// Readlink /proc/<pid>/cwd
	id.CWD, _ = os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))

	// Readlink /proc/<pid>/root
	id.Root, _ = os.Readlink(fmt.Sprintf("/proc/%d/root", pid))

	// Parse /proc/<pid>/stat for start time, session, tty, nice
	stat := readProcFile(pid, "stat")
	fields := parseStatFields(stat)
	if len(fields) > 21 {
		id.Session, _ = strconv.Atoi(fields[5])
		ttyNr, _ := strconv.Atoi(fields[6])
		if ttyNr == 0 {
			id.TTY = "(none)"
		} else {
			id.TTY = fmt.Sprintf("%d", ttyNr)
		}
		id.Nice, _ = strconv.Atoi(fields[18])

		startTicks, _ := strconv.ParseInt(fields[21], 10, 64)
		btime := readBtime()
		if btime > 0 && startTicks > 0 {
			startSec := startTicks / clockTicksPerSec
			id.StartedAt = time.Unix(btime+startSec, 0)
			id.StartedAgo = formatDuration(time.Since(id.StartedAt))
		}
	}

	return id
}

func inspectResources(pid int) Resources {
	r := Resources{}

	// Parse /proc/<pid>/status for memory
	status := readProcFile(pid, "status")
	for _, line := range strings.Split(status, "\n") {
		parts := strings.SplitN(line, ":\t", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], strings.TrimSpace(parts[1])
		switch key {
		case "VmPeak":
			r.VmPeak = parseKB(val)
		case "VmRSS":
			r.VmRSS = parseKB(val)
		case "VmSwap":
			r.VmSwap = parseKB(val)
		case "VmSize":
			r.VmSize = parseKB(val)
		case "voluntary_ctxt_switches":
			r.VoluntaryCSW, _ = strconv.ParseInt(val, 10, 64)
		case "nonvoluntary_ctxt_switches":
			r.InvoluntaryCSW, _ = strconv.ParseInt(val, 10, 64)
		}
	}

	// Shared from /proc/<pid>/statm
	statm := readProcFile(pid, "statm")
	smFields := strings.Fields(statm)
	if len(smFields) > 2 {
		shared, _ := strconv.ParseInt(smFields[2], 10, 64)
		r.Shared = shared * 4096 // page size
	}

	// CPU from /proc/<pid>/stat
	stat := readProcFile(pid, "stat")
	fields := parseStatFields(stat)
	if len(fields) > 14 {
		utime, _ := strconv.ParseInt(fields[13], 10, 64)
		stime, _ := strconv.ParseInt(fields[14], 10, 64)
		r.CPUUserSec = float64(utime) / float64(clockTicksPerSec)
		r.CPUSystemSec = float64(stime) / float64(clockTicksPerSec)

		// CPU% = total_cpu_seconds / process_uptime
		if len(fields) > 21 {
			startTicks, _ := strconv.ParseInt(fields[21], 10, 64)
			btime := readBtime()
			if btime > 0 && startTicks > 0 {
				startSec := startTicks / clockTicksPerSec
				uptime := time.Since(time.Unix(btime+startSec, 0)).Seconds()
				if uptime > 0 {
					r.CPUPercent = (r.CPUUserSec + r.CPUSystemSec) / uptime * 100
				}
			}
		}
	}

	// FD count
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err == nil {
		r.FDsOpen = len(entries)
	}

	// FD limits from /proc/<pid>/limits
	limits := readProcFile(pid, "limits")
	for _, line := range strings.Split(limits, "\n") {
		if strings.HasPrefix(line, "Max open files") {
			fields := strings.Fields(line)
			if len(fields) >= 6 {
				r.FDsSoftLimit, _ = strconv.Atoi(fields[3])
				r.FDsHardLimit, _ = strconv.Atoi(fields[4])
			}
		}
	}

	return r
}

func buildProcessTree(pid int) []TreeNode {
	var chain []TreeNode
	current := pid
	for current > 0 {
		node := TreeNode{PID: current}

		status := readProcFile(current, "status")
		for _, line := range strings.Split(status, "\n") {
			parts := strings.SplitN(line, ":\t", 2)
			if len(parts) != 2 {
				continue
			}
			key, val := parts[0], strings.TrimSpace(parts[1])
			switch key {
			case "PPid":
				node.PPid, _ = strconv.Atoi(val)
			case "Uid":
				fields := strings.Fields(val)
				if len(fields) > 0 {
					if u, err := user.LookupId(fields[0]); err == nil {
						node.User = u.Username
					}
				}
			}
		}

		cmdline := readProcFile(current, "cmdline")
		if cmdline != "" {
			args := strings.Split(strings.TrimRight(cmdline, "\x00"), "\x00")
			node.Cmdline = strings.Join(args, " ")
		}

		exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", current))
		if err == nil {
			node.Exe = exe
		}

		// Start time
		stat := readProcFile(current, "stat")
		fields := parseStatFields(stat)
		if len(fields) > 21 {
			startTicks, _ := strconv.ParseInt(fields[21], 10, 64)
			btime := readBtime()
			if btime > 0 && startTicks > 0 {
				startSec := startTicks / clockTicksPerSec
				node.StartedAt = time.Unix(btime+startSec, 0)
				node.StartedAgo = formatDuration(time.Since(node.StartedAt))
			}
		}

		chain = append(chain, node)
		if current == 1 {
			break
		}
		current = node.PPid
	}
	return chain
}

func inspectFDs(pid int, limit int) []FDInfo {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil
	}

	var fds []FDInfo
	for i, e := range entries {
		if i >= limit {
			break
		}
		fd, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		target, err := os.Readlink(filepath.Join(fdDir, e.Name()))
		if err != nil {
			continue
		}

		info := FDInfo{
			FD:     fd,
			Type:   classifyFD(target),
			Mode:   fdMode(pid, fd),
			Target: target,
		}

		// Extract inode for socket matching
		if strings.HasPrefix(target, "socket:[") {
			inodeStr := target[8 : len(target)-1]
			info.Inode, _ = strconv.Atoi(inodeStr)
		}

		fds = append(fds, info)
	}
	return fds
}

func inspectSockets(pid int, fds []FDInfo) []SocketInfo {
	// Build inode->FD map
	inodeFD := make(map[int]int)
	for _, fd := range fds {
		if fd.Inode > 0 {
			inodeFD[fd.Inode] = fd.FD
		}
	}

	var sockets []SocketInfo

	// TCP
	for _, path := range []string{"net/tcp", "net/tcp6"} {
		content := readProcFile(pid, path)
		for _, line := range strings.Split(content, "\n")[1:] { // skip header
			if s := parseTCPLine(line); s != nil {
				if fd, ok := inodeFD[s.Inode]; ok {
					s.FD = fd
				}
				sockets = append(sockets, *s)
			}
		}
	}

	// UDP
	for _, path := range []string{"net/udp", "net/udp6"} {
		content := readProcFile(pid, path)
		for _, line := range strings.Split(content, "\n")[1:] {
			if s := parseUDPLine(line); s != nil {
				if fd, ok := inodeFD[s.Inode]; ok {
					s.FD = fd
				}
				sockets = append(sockets, *s)
			}
		}
	}

	// Unix sockets
	content := readProcFile(pid, "net/unix")
	for _, line := range strings.Split(content, "\n")[1:] {
		if s := parseUnixLine(line); s != nil {
			if fd, ok := inodeFD[s.Inode]; ok {
				s.FD = fd
			}
			sockets = append(sockets, *s)
		}
	}

	return sockets
}

func inspectEnviron(pid int) map[string]string {
	data := readProcFile(pid, "environ")
	if data == "" {
		return nil
	}
	env := make(map[string]string)
	for _, entry := range strings.Split(strings.TrimRight(data, "\x00"), "\x00") {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	return env
}

func inspectCgroup(pid int) CgroupInfo {
	c := CgroupInfo{}
	c.Path = strings.TrimSpace(readProcFile(pid, "cgroup"))
	c.OOMScore = readIntFile(pid, "oom_score")
	c.OOMAdj = readIntFile(pid, "oom_score_adj")

	status := readProcFile(pid, "status")
	for _, line := range strings.Split(status, "\n") {
		parts := strings.SplitN(line, ":\t", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], strings.TrimSpace(parts[1])
		switch key {
		case "Seccomp":
			c.Seccomp, _ = strconv.Atoi(val)
		case "NoNewPrivs":
			c.NoNewPrivs, _ = strconv.Atoi(val)
		case "CapEff":
			c.CapEff = val
		}
	}
	return c
}

// --- Helpers ---

func readProcFile(pid int, name string) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/%s", pid, name))
	if err != nil {
		return ""
	}
	return string(data)
}

func readIntFile(pid int, name string) int {
	s := strings.TrimSpace(readProcFile(pid, name))
	v, _ := strconv.Atoi(s)
	return v
}

func readBtime() int64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "btime ") {
			v, _ := strconv.ParseInt(strings.TrimPrefix(line, "btime "), 10, 64)
			return v
		}
	}
	return 0
}

// parseStatFields parses /proc/<pid>/stat correctly, handling comm field with parens.
func parseStatFields(stat string) []string {
	// Format: pid (comm) state ppid ...
	// comm can contain spaces and parens, so find the last ')'
	start := strings.IndexByte(stat, '(')
	end := strings.LastIndexByte(stat, ')')
	if start < 0 || end < 0 || end <= start {
		return nil
	}

	var fields []string
	fields = append(fields, stat[:start-1])  // pid
	fields = append(fields, stat[start+1:end]) // comm
	rest := strings.Fields(stat[end+2:])     // state ppid ...
	fields = append(fields, rest...)
	return fields
}

func stateToHuman(s string) string {
	switch s {
	case "R":
		return "running"
	case "S":
		return "sleeping"
	case "D":
		return "disk sleep"
	case "Z":
		return "zombie"
	case "T":
		return "stopped"
	case "t":
		return "tracing stop"
	case "X", "x":
		return "dead"
	default:
		return s
	}
}

func parseKB(s string) int64 {
	// e.g. "45312 kB"
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseInt(fields[0], 10, 64)
	return v * 1024 // kB to bytes
}

func classifyFD(target string) string {
	switch {
	case strings.HasPrefix(target, "pipe:"):
		return "pipe"
	case strings.HasPrefix(target, "socket:"):
		return "socket"
	case strings.HasPrefix(target, "anon_inode:"):
		return "anon"
	case strings.HasPrefix(target, "/dev/"):
		return "/dev"
	default:
		return "regular"
	}
}

func fdMode(pid, fd int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/fdinfo/%d", pid, fd))
	if err != nil {
		return "?"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "flags:\t") {
			flagStr := strings.TrimPrefix(line, "flags:\t")
			flags, err := strconv.ParseUint(strings.TrimSpace(flagStr), 8, 32)
			if err != nil {
				return "?"
			}
			switch flags & 0x3 {
			case 0:
				return "r"
			case 1:
				return "w"
			case 2:
				return "rw"
			}
		}
	}
	return "?"
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

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
	if len(parts) == 0 {
		seconds := int(d.Seconds()) % 60
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}
	return strings.Join(parts, " ")
}

// --- Network parsing ---

var tcpStateMap = map[string]string{
	"01": "ESTABLISHED", "02": "SYN_SENT", "03": "SYN_RECV",
	"04": "FIN_WAIT1", "05": "FIN_WAIT2", "06": "TIME_WAIT",
	"07": "CLOSE", "08": "CLOSE_WAIT", "09": "LAST_ACK",
	"0A": "LISTEN", "0B": "CLOSING",
}

func parseTCPLine(line string) *SocketInfo {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return nil
	}
	local := parseHexAddr(fields[1])
	remote := parseHexAddr(fields[2])
	state := tcpStateMap[fields[3]]
	if state == "" {
		state = fields[3]
	}
	inode, _ := strconv.Atoi(fields[9])
	return &SocketInfo{Proto: "TCP", Local: local, Remote: remote, State: state, Inode: inode}
}

func parseUDPLine(line string) *SocketInfo {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return nil
	}
	local := parseHexAddr(fields[1])
	remote := parseHexAddr(fields[2])
	inode, _ := strconv.Atoi(fields[9])
	state := "UNCONN"
	if remote != "0.0.0.0:0" && remote != ":::0" {
		state = "CONNECTED"
	}
	return &SocketInfo{Proto: "UDP", Local: local, Remote: remote, State: state, Inode: inode}
}

func parseUnixLine(line string) *SocketInfo {
	fields := strings.Fields(line)
	if len(fields) < 7 {
		return nil
	}
	inode, _ := strconv.Atoi(fields[6])
	path := ""
	if len(fields) > 7 {
		path = fields[7]
	}
	return &SocketInfo{Proto: "UNIX", Local: path, State: "CONNECTED", Inode: inode}
}

func parseHexAddr(s string) string {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return s
	}
	ip := parseHexIP(parts[0])
	port, _ := strconv.ParseUint(parts[1], 16, 16)
	return fmt.Sprintf("%s:%d", ip, port)
}

func parseHexIP(hex string) string {
	if len(hex) == 8 {
		// IPv4 little-endian
		val, _ := strconv.ParseUint(hex, 16, 32)
		return fmt.Sprintf("%d.%d.%d.%d", val&0xFF, (val>>8)&0xFF, (val>>16)&0xFF, (val>>24)&0xFF)
	}
	// IPv6 - simplified, return as-is for now
	return hex
}

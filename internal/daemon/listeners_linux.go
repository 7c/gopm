//go:build linux

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// scanProcessListeners finds listening sockets owned by a specific process.
// It cross-references /proc/<pid>/fd/ socket inodes with /proc/<pid>/net/tcp{,6}
// to return only sockets that belong to this process.
func scanProcessListeners(pid int) []string {
	// Step 1: Collect socket inodes owned by this process.
	ownedInodes := processSocketInodes(pid)
	if len(ownedInodes) == 0 {
		return nil
	}

	var listeners []string

	// Step 2: Scan /proc/<pid>/net/tcp{,6} for LISTEN entries matching our inodes.
	for _, entry := range []struct {
		file  string
		proto string
	}{
		{"net/tcp", "tcp"},
		{"net/tcp6", "tcp6"},
	} {
		path := fmt.Sprintf("/proc/%d/%s", pid, entry.file)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n")[1:] { // skip header
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			// Only LISTEN state (0A)
			if fields[3] != "0A" {
				continue
			}
			// Check if inode belongs to this process
			inode, _ := strconv.Atoi(fields[9])
			if inode == 0 || !ownedInodes[inode] {
				continue
			}
			local := parseListenerHexAddr(fields[1])
			listeners = append(listeners, fmt.Sprintf("%s@%s", entry.proto, local))
		}
	}

	// Step 3: Scan UDP for bound sockets (UDP has no LISTEN state).
	for _, entry := range []struct {
		file  string
		proto string
	}{
		{"net/udp", "udp"},
		{"net/udp6", "udp6"},
	} {
		path := fmt.Sprintf("/proc/%d/%s", pid, entry.file)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n")[1:] {
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			inode, _ := strconv.Atoi(fields[9])
			if inode == 0 || !ownedInodes[inode] {
				continue
			}
			local := parseListenerHexAddr(fields[1])
			// Skip unbound (0.0.0.0:0 or :::0)
			if strings.HasSuffix(local, ":0") {
				continue
			}
			listeners = append(listeners, fmt.Sprintf("%s@%s", entry.proto, local))
		}
	}

	return listeners
}

// processSocketInodes reads /proc/<pid>/fd/ and returns a set of socket inodes.
func processSocketInodes(pid int) map[int]bool {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil
	}
	inodes := make(map[int]bool)
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join(fdDir, e.Name()))
		if err != nil {
			continue
		}
		// socket:[12345]
		if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
			inodeStr := target[8 : len(target)-1]
			if inode, err := strconv.Atoi(inodeStr); err == nil {
				inodes[inode] = true
			}
		}
	}
	return inodes
}

func parseListenerHexAddr(s string) string {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return s
	}
	ip := parseListenerHexIP(parts[0])
	port, _ := strconv.ParseUint(parts[1], 16, 16)
	return fmt.Sprintf("%s:%d", ip, port)
}

func parseListenerHexIP(hex string) string {
	if len(hex) == 8 {
		// IPv4 little-endian
		val, _ := strconv.ParseUint(hex, 16, 32)
		return fmt.Sprintf("%d.%d.%d.%d", val&0xFF, (val>>8)&0xFF, (val>>16)&0xFF, (val>>24)&0xFF)
	}
	if len(hex) == 32 {
		// IPv6 - check if it's all zeros (::)
		allZero := true
		for _, c := range hex {
			if c != '0' {
				allZero = false
				break
			}
		}
		if allZero {
			return "::"
		}
		// Check for ::1 (loopback)
		if hex == "00000000000000000000000001000000" {
			return "::1"
		}
		// Parse 4 groups of 4 bytes (little-endian per group)
		var segments [8]uint16
		for i := 0; i < 4; i++ {
			group := hex[i*8 : (i+1)*8]
			val, _ := strconv.ParseUint(group, 16, 32)
			segments[i*2] = uint16((val>>8)&0xFF | (val&0xFF)<<8)
			segments[i*2+1] = uint16((val>>24)&0xFF | ((val>>16)&0xFF)<<8)
		}
		var sb strings.Builder
		for i, p := range segments {
			if i > 0 {
				sb.WriteByte(':')
			}
			fmt.Fprintf(&sb, "%x", p)
		}
		return sb.String()
	}
	return hex
}

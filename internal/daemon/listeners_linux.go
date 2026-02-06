//go:build linux

package daemon

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// scanProcessListeners reads /proc/<pid>/net/tcp{,6} and /proc/<pid>/net/udp{,6}
// and returns listening addresses in "proto@addr:port" format.
func scanProcessListeners(pid int) []string {
	var listeners []string

	for _, entry := range []struct {
		file  string
		proto string
	}{
		{"net/tcp", "tcp"},
		{"net/tcp6", "tcp6"},
		{"net/udp", "udp"},
		{"net/udp6", "udp6"},
	} {
		path := fmt.Sprintf("/proc/%d/%s", pid, entry.file)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines[1:] { // skip header
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			// TCP: state 0A = LISTEN; UDP: any bound socket
			if strings.HasPrefix(entry.proto, "tcp") {
				if fields[3] != "0A" { // not LISTEN
					continue
				}
			} else {
				// UDP: skip if local addr is 0.0.0.0:0 or :::0
				local := parseListenerHexAddr(fields[1])
				if local == "0.0.0.0:0" || local == ":::0" {
					continue
				}
			}
			local := parseListenerHexAddr(fields[1])
			listeners = append(listeners, fmt.Sprintf("%s@%s", entry.proto, local))
		}
	}

	return listeners
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
		// Parse 4 groups of 4 bytes (little-endian per group)
		var parts [8]uint16
		for i := 0; i < 4; i++ {
			group := hex[i*8 : (i+1)*8]
			val, _ := strconv.ParseUint(group, 16, 32)
			// Reverse byte order within each 32-bit group
			parts[i*2] = uint16((val>>8)&0xFF | (val&0xFF)<<8)
			parts[i*2+1] = uint16((val>>24)&0xFF | ((val>>16)&0xFF)<<8)
		}
		var sb strings.Builder
		for i, p := range parts {
			if i > 0 {
				sb.WriteByte(':')
			}
			fmt.Fprintf(&sb, "%x", p)
		}
		return sb.String()
	}
	return hex
}

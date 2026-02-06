//go:build darwin

package daemon

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// scanProcessListeners uses lsof to find listening TCP sockets for a process.
func scanProcessListeners(pid int) []string {
	out, err := exec.Command("lsof", "-iTCP", "-sTCP:LISTEN", "-nP", "-a", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return nil
	}

	var listeners []string
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines[1:] { // skip header
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		// Field 8 is "NAME" e.g. "*:3000" or "127.0.0.1:8080"
		name := fields[8]
		// Convert lsof format to our format
		addr := name
		if strings.HasPrefix(addr, "*:") {
			addr = "0.0.0.0:" + addr[2:]
		}
		listeners = append(listeners, fmt.Sprintf("tcp@%s", addr))
	}

	return listeners
}

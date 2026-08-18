//go:build linux

package daemon

import (
	"os"
	"path/filepath"
	"strings"
)

// isZombieLinux reads /proc/<pid>/stat and reports true when the process
// state field is 'Z'. Used by test helpers to distinguish a truly-alive
// PID from one that's dead but not yet reaped.
func isZombieLinux(pid int) bool {
	data, err := os.ReadFile(filepath.Join("/proc", itoa(pid), "stat"))
	if err != nil {
		return true // process gone → treat as zombie/gone
	}
	// Format: "PID (comm) STATE ..." where comm may contain spaces or
	// parens. Find the last ')' to skip the comm field safely.
	s := string(data)
	if i := strings.LastIndex(s, ") "); i > 0 && len(s) > i+2 {
		return s[i+2] == 'Z'
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

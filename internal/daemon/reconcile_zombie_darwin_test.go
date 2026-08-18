//go:build darwin

package daemon

import (
	"os/exec"
	"strings"
)

// isZombieLinux is a portable stub on Darwin: `ps -p <pid> -o stat=` shows
// state; 'Z' means zombie. If ps returns nothing the pid is gone.
// Rename kept for test-file symmetry — the caller only cares about "is
// this pid essentially gone", regardless of OS.
func isZombieLinux(pid int) bool {
	out, err := exec.Command("ps", "-p", itoa(pid), "-o", "stat=").Output()
	if err != nil {
		return true // ps errored → pid gone
	}
	state := strings.TrimSpace(string(out))
	if state == "" {
		return true
	}
	return strings.HasPrefix(state, "Z")
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

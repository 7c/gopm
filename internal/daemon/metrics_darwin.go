//go:build darwin

package daemon

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// sampleProcessMetrics reads CPU and memory using ps on macOS.
func sampleProcessMetrics(pid int) (rss uint64, cpuTicks uint64, err error) {
	out, err := exec.Command("ps", "-o", "rss=,cputime=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) >= 1 {
		kb, _ := strconv.ParseUint(fields[0], 10, 64)
		rss = kb * 1024
	}
	// On macOS, we use RSS directly and return 0 for cpuTicks
	// CPU percentage is calculated differently
	return rss, 0, nil
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}

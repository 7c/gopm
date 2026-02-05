//go:build linux

package daemon

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// sampleProcessMetrics reads CPU and memory from /proc for a given PID.
func sampleProcessMetrics(pid int) (rss uint64, cpuTicks uint64, err error) {
	// Memory: read /proc/<pid>/status → VmRSS line → parse KB → bytes
	statusPath := fmt.Sprintf("/proc/%d/status", pid)
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseUint(fields[1], 10, 64)
				rss = kb * 1024
			}
			break
		}
	}

	// CPU: read /proc/<pid>/stat → fields 14(utime)+15(stime)
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	statData, err := os.ReadFile(statPath)
	if err != nil {
		return rss, 0, err
	}
	// Find the closing ')' to skip the comm field which may contain spaces
	idx := strings.LastIndex(string(statData), ")")
	if idx < 0 {
		return rss, 0, fmt.Errorf("invalid /proc/%d/stat format", pid)
	}
	fields := strings.Fields(string(statData)[idx+2:])
	if len(fields) >= 13 {
		utime, _ := strconv.ParseUint(fields[11], 10, 64)
		stime, _ := strconv.ParseUint(fields[12], 10, 64)
		cpuTicks = utime + stime
	}

	return rss, cpuTicks, nil
}

func processExists(pid int) bool {
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return err == nil
}

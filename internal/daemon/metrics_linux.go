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

// countDescendants returns the total number of descendant processes of pid
// (direct children, grandchildren, etc). On Linux, this is done via
// /proc/<pid>/task/<tid>/children which lists direct children per task.
func countDescendants(pid int) int {
	total := 0
	var walk func(int)
	walk = func(parent int) {
		taskDir := fmt.Sprintf("/proc/%d/task", parent)
		entries, err := os.ReadDir(taskDir)
		if err != nil {
			return
		}
		for _, e := range entries {
			childrenPath := fmt.Sprintf("/proc/%d/task/%s/children", parent, e.Name())
			data, err := os.ReadFile(childrenPath)
			if err != nil {
				continue
			}
			for _, f := range strings.Fields(string(data)) {
				cpid, err := strconv.Atoi(f)
				if err != nil {
					continue
				}
				total++
				walk(cpid)
			}
		}
	}
	walk(pid)
	return total
}

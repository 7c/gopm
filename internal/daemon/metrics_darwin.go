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

// countDescendants returns the total number of descendant processes of pid
// (children, grandchildren, ...). On macOS we shell out to `ps` once to get
// all (pid, ppid) pairs and walk the tree in-memory.
func countDescendants(pid int) int {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		return 0
	}
	children := make(map[int][]int)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		cpid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		children[ppid] = append(children[ppid], cpid)
	}
	total := 0
	var walk func(int)
	walk = func(p int) {
		for _, c := range children[p] {
			total++
			walk(c)
		}
	}
	walk(pid)
	return total
}

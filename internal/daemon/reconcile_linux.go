//go:build linux

package daemon

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
)

// findOrphans walks /proc for live PIDs whose environment contains
// GOPM_MANAGED_NAME=<fp.Name>. Env-marker matching is exact (NUL-bounded)
// so foo doesn't match foo-bar. Returns nil if /proc is unreadable.
// Skips the daemon's own PID.
func findOrphans(fp orphanFingerprint) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		slog.Warn("reconcile: /proc unreadable — cannot scan for orphans",
			"error", err)
		return nil
	}
	self := os.Getpid()
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a numeric PID entry
		}
		if pid == self {
			continue
		}
		envBlob, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ"))
		if err != nil {
			// Common: process died, or another user's process (EACCES).
			continue
		}
		if envTokenMatches(envBlob, managedNameEnv, fp.Name, 0) {
			pids = append(pids, pid)
		}
	}
	return pids
}

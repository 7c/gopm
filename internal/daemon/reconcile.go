package daemon

import (
	"log/slog"
	"sort"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// managedNameEnv and managedIDEnv are injected into every child process by
// Process.Start(). On Linux they are the identity signal reconcile scans
// for via /proc/<pid>/environ. On Darwin they are still set (useful for
// operator debugging via `ps -E` when SIP allows) but the primary Darwin
// identity signal is argv, since modern macOS SIP hides env from foreign
// processes.
const (
	managedNameEnv = "GOPM_MANAGED_NAME"
	managedIDEnv   = "GOPM_MANAGED_ID"
)

// defaultOrphanKillTimeout bounds how long reconcile waits after SIGTERM
// before escalating to SIGKILL. Keep it short — daemon startup is blocked
// on this per managed process.
const defaultOrphanKillTimeout = 5 * time.Second

// orphanReconcileCount counts pgroups killed by reconcileOrphans across the
// daemon's lifetime. Exposed for status/telemetry.
var orphanReconcileCount uint64

// OrphanReconcileCount returns the current value.
func OrphanReconcileCount() uint64 { return atomic.LoadUint64(&orphanReconcileCount) }

// orphanFingerprint carries the identity signals findOrphans uses. Linux
// only needs Name (env-marker match). Darwin needs Command+Args (argv
// match, since env is hidden by SIP on modern macOS). We always pass all
// three so the OS-specific implementation can pick.
type orphanFingerprint struct {
	Name    string
	Command string
	Args    []string
}

func fingerprintFor(info protocol.ProcessInfo) orphanFingerprint {
	return orphanFingerprint{
		Name:    info.Name,
		Command: info.Command,
		Args:    info.Args,
	}
}

// reconcileOrphans finds any live process matching the fingerprint, groups
// them by current PGID, and kills each group (SIGTERM → wait → SIGKILL).
// Returns the number of PGIDs signalled.
//
// This is the fix for a specific class of duplicate: the previous daemon
// died without running its shutdown path (SIGKILL, OOM, host crash), its
// children survived as orphans (their process groups are intact because
// we spawn with Setpgid), and the new daemon would otherwise spawn a
// second copy on top of them from dump.json.
//
// Every step emits a slog line so daemon.log tells the full story.
// killTimeout <= 0 selects defaultOrphanKillTimeout.
func reconcileOrphans(fp orphanFingerprint, killTimeout time.Duration) int {
	if killTimeout <= 0 {
		killTimeout = defaultOrphanKillTimeout
	}

	pids := findOrphans(fp)
	if len(pids) == 0 {
		slog.Debug("reconcile: no orphans found", "name", fp.Name)
		return 0
	}

	// Group by current PGID. Getpgid reflects setsid()/setpgid() the child
	// may have done post-fork, so this catches processes that daemonized
	// themselves into a new session.
	pgidSet := make(map[int]struct{})
	for _, pid := range pids {
		pgid, err := syscall.Getpgid(pid)
		if err != nil || pgid <= 0 {
			// Race: process died between scan and Getpgid, or permission
			// denied. Fall back to signalling the PID directly.
			slog.Warn("reconcile: could not resolve pgid, falling back to pid",
				"name", fp.Name, "pid", pid, "error", err)
			pgid = pid
		}
		pgidSet[pgid] = struct{}{}
	}

	pgids := sortedKeys(pgidSet)
	slog.Info("reconcile: orphan(s) detected — killing pgroup(s)",
		"name", fp.Name,
		"orphan_pids", pids,
		"pgids", pgids,
		"kill_timeout", killTimeout,
	)

	// Phase 1: SIGTERM every pgroup.
	for _, pgid := range pgids {
		if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
			if err == syscall.ESRCH {
				slog.Debug("reconcile: pgroup already gone at SIGTERM",
					"name", fp.Name, "pgid", pgid)
			} else {
				slog.Warn("reconcile: SIGTERM to pgroup failed",
					"name", fp.Name, "pgid", pgid, "error", err)
			}
		} else {
			slog.Info("reconcile: sent SIGTERM to orphan pgroup",
				"name", fp.Name, "pgid", pgid)
		}
	}

	// Phase 2: poll for exit until killTimeout expires.
	deadline := time.Now().Add(killTimeout)
	pollInterval := 100 * time.Millisecond
	for time.Now().Before(deadline) {
		if len(findOrphans(fp)) == 0 {
			atomic.AddUint64(&orphanReconcileCount, uint64(len(pgids)))
			slog.Info("reconcile: all orphans exited after SIGTERM",
				"name", fp.Name, "pgids_killed", len(pgids))
			return len(pgids)
		}
		time.Sleep(pollInterval)
	}

	// Phase 3: SIGKILL survivors.
	survivors := findOrphans(fp)
	slog.Warn("reconcile: SIGTERM grace expired, escalating to SIGKILL",
		"name", fp.Name, "kill_timeout", killTimeout, "survivor_pids", survivors)

	for _, pgid := range pgids {
		if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
			if err == syscall.ESRCH {
				continue
			}
			slog.Warn("reconcile: SIGKILL to pgroup failed",
				"name", fp.Name, "pgid", pgid, "error", err)
		} else {
			slog.Info("reconcile: sent SIGKILL to orphan pgroup",
				"name", fp.Name, "pgid", pgid)
		}
	}

	// Give the kernel a moment to reap.
	time.Sleep(200 * time.Millisecond)

	final := findOrphans(fp)
	if len(final) > 0 {
		// SIGKILL is unblockable, so if we're still seeing survivors the
		// most likely cause is a scan race (a new orphan spawned during
		// reconcile — shouldn't happen at daemon startup — or a permission
		// issue we can't fix). Log loudly.
		slog.Error("reconcile: orphans still alive after SIGKILL — manual intervention may be needed",
			"name", fp.Name, "surviving_pids", final)
	} else {
		slog.Info("reconcile: complete after SIGKILL",
			"name", fp.Name, "pgids_killed", len(pgids))
	}
	atomic.AddUint64(&orphanReconcileCount, uint64(len(pgids)))
	return len(pgids)
}

func sortedKeys(m map[int]struct{}) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// envTokenMatches reports whether `haystack` (the raw environ blob, either
// NUL-separated on Linux or space-separated from `ps -E`) contains a token
// of the exact form KEY=VALUE, with a proper separator (sep) or end-of-input
// on both sides so KEY=foo doesn't match KEY=foo-bar.
func envTokenMatches(haystack []byte, key, value string, sep byte) bool {
	needle := []byte(key + "=" + value)
	n := len(needle)
	for i := 0; i+n <= len(haystack); i++ {
		if !byteSliceEqual(haystack[i:i+n], needle) {
			continue
		}
		if i > 0 && haystack[i-1] != sep {
			continue
		}
		if i+n < len(haystack) && haystack[i+n] != sep {
			continue
		}
		return true
	}
	return false
}

func byteSliceEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// argvMatches reports whether the process's argv (as returned by
// KERN_PROCARGS2 on Darwin or /proc/<pid>/cmdline on Linux) corresponds
// to the given command+args. We ignore argv[0] because the exec name a
// process reports in argv[0] can differ from the exec path (e.g. shells
// that rewrite it).
func argvMatches(argv []string, command string, args []string) bool {
	if len(argv) == 0 {
		return false
	}
	// argv[1:] must equal args exactly.
	if len(argv)-1 != len(args) {
		return false
	}
	for i, a := range args {
		if argv[1+i] != a {
			return false
		}
	}
	// argv[0] should be the command (or its basename). Accept either
	// exact match or basename match to tolerate exec.Command's absolute
	// path vs a symlink/PATH resolution.
	if argv[0] == command {
		return true
	}
	return baseName(argv[0]) == baseName(command)
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

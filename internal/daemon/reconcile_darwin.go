//go:build darwin

package daemon

import (
	"encoding/binary"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// findOrphans enumerates every process visible via `ps -A -o pid=`, reads
// argv via KERN_PROCARGS2, and returns PIDs whose argv matches
// fp.Command + fp.Args. Env-based matching is not viable on modern macOS:
// SIP truncates KERN_PROCARGS2 env for cross-process reads, so we identify
// orphans by argv shape instead.
//
// This is a slightly weaker signal than the Linux env-marker path (a user
// running the same binary with the same args outside gopm would look like
// an orphan) but it's the strongest identity signal the kernel gives us on
// current macOS.
func findOrphans(fp orphanFingerprint) []int {
	out, err := exec.Command("ps", "-A", "-o", "pid=").Output()
	if err != nil {
		slog.Warn("reconcile: ps -A failed — cannot enumerate PIDs",
			"error", err)
		return nil
	}
	self := os.Getpid()
	var pids []int
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err != nil || pid == self {
			continue
		}
		argv, err := procArgvDarwin(pid)
		if err != nil {
			continue // died, restricted, or foreign uid
		}
		if argvMatches(argv, fp.Command, fp.Args) {
			pids = append(pids, pid)
		}
	}
	return pids
}

// procArgvDarwin returns argv for pid via KERN_PROCARGS2.
// Layout of the returned buffer:
//
//	uint32     argc
//	char[]     exec_path (NUL-terminated, then zero-padded)
//	char[]     argv[0]\0 argv[1]\0 ... argv[argc-1]\0
//	char[]     env[0]\0 env[1]\0 ...      (env; may be truncated by SIP)
//
// We return the argv slice only. Env is unreliable on modern macOS so we
// don't attempt to expose it here.
func procArgvDarwin(pid int) ([]string, error) {
	buf, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, err
	}
	if len(buf) < 4 {
		return nil, os.ErrInvalid
	}
	argc := int(binary.LittleEndian.Uint32(buf[:4]))
	if argc < 0 || argc > 4096 {
		return nil, os.ErrInvalid
	}

	// Skip exec_path (may be preceded and followed by padding NULs).
	i := 4
	for i < len(buf) && buf[i] == 0 {
		i++
	}
	for i < len(buf) && buf[i] != 0 {
		i++
	}
	for i < len(buf) && buf[i] == 0 {
		i++
	}

	argv := make([]string, 0, argc)
	for n := 0; n < argc && i < len(buf); n++ {
		start := i
		for i < len(buf) && buf[i] != 0 {
			i++
		}
		argv = append(argv, string(buf[start:i]))
		i++ // skip NUL
	}
	if len(argv) != argc {
		return nil, os.ErrInvalid
	}
	return argv, nil
}

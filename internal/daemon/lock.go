package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// daemonLockName is the file whose exclusive flock guarantees at most one
// daemon per $GOPM_HOME. Kept as a plain file (not the socket or PID file)
// because os.Remove on the socket during startup would race with a
// competing daemon that took our lock, and PID files aren't self-cleaning
// after a crash — flock is: the kernel releases it when the FD closes.
const daemonLockName = "daemon.lock"

// errDaemonAlreadyRunning is returned by acquireDaemonLock when another
// daemon holds the exclusive lock. Callers should treat this as a
// clean "nothing to do" exit (code 0), not an error — the intended
// state (one daemon running for this $GOPM_HOME) is already satisfied.
var errDaemonAlreadyRunning = errors.New("another gopm daemon is already running for this $GOPM_HOME")

// acquireDaemonLock opens $GOPM_HOME/daemon.lock and takes a non-blocking
// exclusive flock on it. On success returns the open *os.File — keep it
// alive for the daemon's lifetime; closing it releases the lock. On
// contention returns errDaemonAlreadyRunning.
//
// This is the single-instance guard that closes the concurrent-startup
// race reconcile cannot cover: two `gopm --daemon` invocations in the
// same instant both boot before either has spawned children, so
// reconcile finds nothing for either to sweep — and then both proceed
// to spawn full sets of children with env markers.
func acquireDaemonLock(home string) (*os.File, error) {
	path := filepath.Join(home, daemonLockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// LOCK_EX exclusive, LOCK_NB non-blocking. If the lock is held we get
	// EWOULDBLOCK immediately and can exit — never queue behind another
	// daemon's startup, which could serialize a burst of CLI-triggered
	// daemon spawns into a slow cascade.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, errDaemonAlreadyRunning
		}
		return nil, fmt.Errorf("flock %s: %w", path, err)
	}
	return f, nil
}

//go:build linux

package test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
)

// readGopmHome returns the pid's GOPM_HOME env value on Linux via
// /proc/<pid>/environ. Returns (value, true) on success, ("", false)
// when the env can't be read (foreign uid, race with process exit, etc.).
func readGopmHome(pid int) (string, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ"))
	if err != nil {
		return "", false
	}
	prefix := []byte("GOPM_HOME=")
	for _, kv := range bytes.Split(data, []byte{0}) {
		if bytes.HasPrefix(kv, prefix) {
			return string(kv[len(prefix):]), true
		}
	}
	return "", true // env readable but no GOPM_HOME set — treat as "matches nothing"
}

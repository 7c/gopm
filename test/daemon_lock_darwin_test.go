//go:build darwin

package test

// readGopmHome on Darwin can't return the env: modern macOS SIP hides
// KERN_PROCARGS2 env from cross-process reads. The test falls back to
// command-line matching, which is safe because each TestEnv gets a
// unique /tmp/gp-XXXXXX GOPM_HOME path.
func readGopmHome(pid int) (string, bool) {
	return "", false
}

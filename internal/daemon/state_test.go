package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/7c/gopm/internal/protocol"
)

func TestShouldResurrectAsRunning(t *testing.T) {
	cases := []struct {
		name string
		info protocol.ProcessInfo
		want bool
	}{
		{
			name: "online is resurrected",
			info: protocol.ProcessInfo{Status: protocol.StatusOnline},
			want: true,
		},
		{
			name: "stopped stays stopped",
			info: protocol.ProcessInfo{Status: protocol.StatusStopped},
			want: false,
		},
		{
			name: "errored stays errored",
			info: protocol.ProcessInfo{Status: protocol.StatusErrored},
			want: false,
		},
		{
			// Regression: `gopm reboot` fired inside the supervisor's
			// restart-delay window persisted status=stopped and
			// in_restart_delay=true; resurrect used to skip these, leaving
			// crash-looping processes orphaned as "stopped" until a manual
			// `gopm start`. See internal/daemon/supervisor.go where
			// InRestartDelay is set before the delay begins.
			name: "stopped in restart delay is resurrected",
			info: protocol.ProcessInfo{
				Status:         protocol.StatusStopped,
				InRestartDelay: true,
			},
			want: true,
		},
		{
			// Defensive: if an errored process somehow carried the delay
			// flag, honoring it is safer than orphaning — the supervisor
			// only sets the flag when it intended to restart.
			name: "errored in restart delay is resurrected",
			info: protocol.ProcessInfo{
				Status:         protocol.StatusErrored,
				InRestartDelay: true,
			},
			want: true,
		},
		{
			name: "online with stale delay flag is resurrected",
			info: protocol.ProcessInfo{
				Status:         protocol.StatusOnline,
				InRestartDelay: true,
			},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldResurrectAsRunning(tc.info); got != tc.want {
				t.Errorf("shouldResurrectAsRunning(%+v) = %v, want %v", tc.info, got, tc.want)
			}
		})
	}
}

// TestStopClearsRestartDelayFlag verifies that Stop() eagerly clears
// InRestartDelay=false when it cancels a pending supervisor restart, so a
// SaveState racing with the supervisor's post-cancel cleanup can't persist a
// user-stopped process as "still in restart delay" — which would otherwise
// be interpreted as "resume on resurrect" by ResurrectProcesses and start
// the process against the user's explicit stop.
func TestStopClearsRestartDelayFlag(t *testing.T) {
	// Simulate a process the supervisor has just parked in the restart-delay
	// window: cancelRestart armed, InRestartDelay=true, Status=stopped. No
	// cmd is running so Stop() takes the early-return path (the branch where
	// the race actually lives).
	p := &Process{
		info: protocol.ProcessInfo{
			Name:           "delayed",
			Status:         protocol.StatusStopped,
			InRestartDelay: true,
		},
		cancelRestart: make(chan struct{}),
	}

	if err := p.Stop("stopped by user"); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	info := p.Info()
	if info.InRestartDelay {
		t.Errorf("InRestartDelay = true after Stop, want false — SaveState racing here would orphan the user-stop intent")
	}
	if info.Status != protocol.StatusStopped {
		t.Errorf("Status = %q, want %q", info.Status, protocol.StatusStopped)
	}
	if info.StatusReason != "stopped by user" {
		t.Errorf("StatusReason = %q, want %q", info.StatusReason, "stopped by user")
	}
	select {
	case <-p.cancelRestart:
	default:
		t.Error("cancelRestart channel was not closed by Stop")
	}
	if shouldResurrectAsRunning(info) {
		t.Error("shouldResurrectAsRunning returned true for a user-stopped process")
	}
}

// TestLoadStatePreservesInRestartDelay verifies the on-disk dump.json roundtrip
// preserves the in_restart_delay flag. This guards against a serialization
// regression that would silently re-introduce the orphaning bug.
func TestLoadStatePreservesInRestartDelay(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPM_HOME", home)

	original := []protocol.ProcessInfo{
		{
			ID:             3,
			Name:           "crashloop",
			Command:        "/bin/false",
			Status:         protocol.StatusStopped,
			InRestartDelay: true,
			RestartPolicy:  protocol.DefaultRestartPolicy(),
		},
	}

	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "dump.json"), data, 0644); err != nil {
		t.Fatalf("write dump: %v", err)
	}

	loaded, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded))
	}
	if !loaded[0].InRestartDelay {
		t.Errorf("InRestartDelay lost on roundtrip: %+v", loaded[0])
	}
	if loaded[0].Status != protocol.StatusStopped {
		t.Errorf("Status = %q, want %q", loaded[0].Status, protocol.StatusStopped)
	}
	if !shouldResurrectAsRunning(loaded[0]) {
		t.Error("loaded entry should be classified as running-on-resurrect")
	}
}

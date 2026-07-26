package cli

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

func TestNormalizeTargets_SinglePreserved(t *testing.T) {
	got := normalizeTargets([]string{"api"})
	want := []string{"api"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("normalizeTargets(%v) = %v, want %v", []string{"api"}, got, want)
	}
}

func TestNormalizeTargets_MultiplePreservesOrder(t *testing.T) {
	got := normalizeTargets([]string{"c", "a", "b"})
	want := []string{"c", "a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order not preserved: got %v, want %v", got, want)
	}
}

func TestNormalizeTargets_Deduplicates(t *testing.T) {
	got := normalizeTargets([]string{"a", "b", "a", "c", "b"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dedup failed: got %v, want %v", got, want)
	}
}

func TestNormalizeTargets_AllShortCircuits(t *testing.T) {
	// "all" anywhere in the arg list collapses to just ["all"] — otherwise
	// we'd stop the same processes twice (once by name, once via "all").
	cases := [][]string{
		{"all"},
		{"all", "api"},
		{"api", "all"},
		{"api", "worker", "all", "cron"},
	}
	for _, in := range cases {
		got := normalizeTargets(in)
		want := []string{"all"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("normalizeTargets(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestStopCmd_AcceptsMultipleArgs(t *testing.T) {
	// cobra.MinimumNArgs(1) allows 1..N args.
	if err := stopCmd.Args(&cobra.Command{}, []string{"a"}); err != nil {
		t.Errorf("expected 1 arg to be accepted: %v", err)
	}
	if err := stopCmd.Args(&cobra.Command{}, []string{"a", "b", "c"}); err != nil {
		t.Errorf("expected multiple args to be accepted: %v", err)
	}
	if err := stopCmd.Args(&cobra.Command{}, nil); err == nil {
		t.Error("expected zero args to be rejected")
	}
}

func TestRestartCmd_AcceptsMultipleArgs(t *testing.T) {
	if err := restartCmd.Args(&cobra.Command{}, []string{"a"}); err != nil {
		t.Errorf("expected 1 arg to be accepted: %v", err)
	}
	if err := restartCmd.Args(&cobra.Command{}, []string{"a", "b"}); err != nil {
		t.Errorf("expected multiple args to be accepted: %v", err)
	}
	if err := restartCmd.Args(&cobra.Command{}, nil); err == nil {
		t.Error("expected zero args to be rejected")
	}
}

func TestUnmarshalRestartResponse_Single(t *testing.T) {
	info := protocol.ProcessInfo{Name: "api", ID: 1}
	data, _ := json.Marshal(info)

	got := unmarshalRestartResponse(data)
	if len(got) != 1 {
		t.Fatalf("expected 1 process, got %d", len(got))
	}
	if got[0].Name != "api" {
		t.Errorf("name = %q, want api", got[0].Name)
	}
}

func TestUnmarshalRestartResponse_Array(t *testing.T) {
	infos := []protocol.ProcessInfo{
		{Name: "api", ID: 1},
		{Name: "worker", ID: 2},
	}
	data, _ := json.Marshal(infos)

	got := unmarshalRestartResponse(data)
	if len(got) != 2 {
		t.Fatalf("expected 2 processes, got %d", len(got))
	}
	if got[0].Name != "api" || got[1].Name != "worker" {
		t.Errorf("unexpected names: %v", got)
	}
}

func TestUnmarshalRestartResponse_EmptyArray(t *testing.T) {
	got := unmarshalRestartResponse(json.RawMessage(`[]`))
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

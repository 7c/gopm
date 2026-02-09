package cli

import (
	"testing"
)

func TestStatsCmd_Flags(t *testing.T) {
	f := statsCmd.Flags()

	hoursFlag := f.Lookup("hours")
	if hoursFlag == nil {
		t.Fatal("expected --hours flag")
	}
	if hoursFlag.DefValue != "6" {
		t.Errorf("expected default '6', got %q", hoursFlag.DefValue)
	}

	cpuFlag := f.Lookup("cpu")
	if cpuFlag == nil {
		t.Fatal("expected --cpu flag")
	}
	if cpuFlag.DefValue != "false" {
		t.Errorf("expected default 'false', got %q", cpuFlag.DefValue)
	}

	memFlag := f.Lookup("mem")
	if memFlag == nil {
		t.Fatal("expected --mem flag")
	}

	uptimeFlag := f.Lookup("uptime")
	if uptimeFlag == nil {
		t.Fatal("expected --uptime flag")
	}

	allFlag := f.Lookup("all")
	if allFlag == nil {
		t.Fatal("expected --all flag")
	}
}

func TestStatsCmd_Args(t *testing.T) {
	if err := statsCmd.Args(statsCmd, []string{}); err != nil {
		t.Errorf("0 args should be valid: %v", err)
	}
	if err := statsCmd.Args(statsCmd, []string{"api"}); err != nil {
		t.Errorf("1 arg should be valid: %v", err)
	}
	if err := statsCmd.Args(statsCmd, []string{"api", "worker"}); err == nil {
		t.Error("2 args should be invalid")
	}
}

func TestStatsCmd_Use(t *testing.T) {
	if statsCmd.Use != "stats [all|name|id]" {
		t.Errorf("unexpected Use: %q", statsCmd.Use)
	}
}

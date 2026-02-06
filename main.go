package main

import (
	_ "embed"
	"strings"

	"github.com/7c/gopm/internal/cli"
)

// Version is set via ldflags at build time, or falls back to embedded version.txt.
var Version = "dev"

//go:embed version.txt
var embeddedVersion string

func main() {
	if Version == "dev" {
		if v := strings.TrimSpace(embeddedVersion); v != "" {
			Version = v
		}
	}
	cli.Version = Version
	cli.Execute()
}

package main

import (
	"runtime/debug"

	"github.com/7c/gopm/internal/cli"
)

// Version is set via ldflags at build time.
var Version = "dev"

func main() {
	if Version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			Version = info.Main.Version
		}
	}
	cli.Version = Version
	cli.Execute()
}

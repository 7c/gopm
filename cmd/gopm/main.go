package main

import (
	"github.com/7c/gopm/internal/cli"
)

// Version is set via ldflags at build time.
var Version = "dev"

func main() {
	cli.Version = Version
	cli.Execute()
}

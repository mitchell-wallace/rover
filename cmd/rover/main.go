// Command rover is the Rover CLI entrypoint; it delegates to internal/cmd.
package main

import (
	"os"

	"github.com/mitchell-wallace/rover/internal/cmd"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := cmd.Execute(version); err != nil {
		os.Exit(1)
	}
}

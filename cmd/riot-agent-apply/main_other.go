//go:build !linux

// Command riot-agent-apply is Linux-only: it relies on O_NOFOLLOW, fstat, and
// systemd. This stub keeps `go build ./...` green on other platforms.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "riot-agent-apply is supported on Linux only")
	os.Exit(1)
}

//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() { fmt.Fprintln(os.Stderr, "riot-maintenance requires Linux apt/systemd"); os.Exit(1) }

//go:build linux

package main

import "testing"

func TestClosedArguments(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--request", "/etc/shadow"}, {"repair"}, {"--env=APT_CONFIG=/attacker"}, {";sh"}} {
		t.Run("[MAINT-RECOVERY AC-001] rejects caller-selected production arguments", func(t *testing.T) {
			if err := run(args); err == nil {
				t.Fatalf("accepted %q", args)
			}
		})
	}
}

package agent

import (
	"context"
	"os"
	"time"
)

func checkMaintenancePolicy() {
	section("Safe OS Maintenance")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	run := execRunner
	if os.Geteuid() == 0 {
		// Listing root's own policy would falsely prove the unprivileged
		// service can start maintenance. -U is read-only policy inspection.
		run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "sudo" && len(args) >= 2 && args[0] == "-n" && args[1] == "-l" {
				args = append([]string{"-n", "-U", "riot", "-l"}, args[2:]...)
			}
			return execRunner(ctx, name, args...)
		}
	}
	if err := maintenanceReadiness(ctx, run); err != nil {
		fail("safe maintenance policy unavailable: %v", err)
		warn("Re-run the independently verified root installer; agent-only updates do not replace privileged helpers or sudoers")
		return
	}
	pass("fixed maintenance service, root helper, sudo 1.9.10+ constrained hold rules and lock-aware signed apply protocol verified")
	probe := &Agent{}
	if blocker := probe.maintenanceBlocker(ctx); blocker != "" {
		warn("conflicting host actions are blocked: %s", blocker)
	}
}

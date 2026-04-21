package collectors

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
	psnet "github.com/shirou/gopsutil/v3/net"
)

type SecurityCollector struct {
	// authCounter is the shared counter populated by LogsCollector on each
	// interval. SecurityCollector drains it to compute FailedLoginsInterval.
	// LogsCollector MUST be registered and collected before SecurityCollector
	// within each collectAll pass (AD-002, SEC-005 serialization invariant).
	authCounter *authFailureCounter
}

func (c *SecurityCollector) Name() string { return "security" }

func (c *SecurityCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.SecurityInfo{
		SELinux:  "N/A",
		AppArmor: "N/A",
	}

	if runtime.GOOS != "linux" {
		// Non-Linux: leave FailedLoginsInterval nil → omitted from JSON (FR-011, AD-008).
		return info, nil
	}

	// SELinux
	if out, err := exec.CommandContext(ctx, "getenforce").Output(); err == nil {
		info.SELinux = strings.TrimSpace(string(out))
	}

	// AppArmor
	if out, err := exec.CommandContext(ctx, "aa-status", "--enabled").Output(); err == nil {
		s := strings.TrimSpace(string(out))
		if s == "Yes" {
			info.AppArmor = "enabled"
		} else {
			info.AppArmor = "disabled"
		}
	}

	// Firewall
	info.FirewallStatus = "inactive"
	if out, err := exec.CommandContext(ctx, "ufw", "status").Output(); err == nil {
		if strings.Contains(string(out), "active") {
			info.FirewallStatus = "active"
		}
	} else if out, err := exec.CommandContext(ctx, "firewall-cmd", "--state").Output(); err == nil {
		info.FirewallStatus = strings.TrimSpace(string(out))
	}

	// Open ports (listening)
	if conns, err := psnet.ConnectionsWithContext(ctx, "tcp"); err == nil {
		portSet := make(map[int]bool)
		for _, conn := range conns {
			if conn.Status == "LISTEN" && !portSet[int(conn.Laddr.Port)] {
				portSet[int(conn.Laddr.Port)] = true
				info.OpenPorts = append(info.OpenPorts, int(conn.Laddr.Port))
			}
		}
	}

	// Logged-in users
	if out, err := exec.CommandContext(ctx, "who").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if lines[0] != "" {
			info.LoggedInUsers = len(lines)
		}
	}

	// Failed logins (last 24h) — pre-existing path; pattern set and logic are
	// frozen per FR-009, BR-001. Do NOT apply the SEC-001 origin filter here.
	if out, err := exec.CommandContext(ctx, "journalctl", "--since", "24 hours ago",
		"-u", "sshd", "--no-pager", "-q").Output(); err == nil {
		count := 0
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "Failed password") || strings.Contains(line, "authentication failure") {
				count++
			}
		}
		info.FailedLogins24h = count
	} else {
		// Fallback: grep auth log files directly.
		// Debian/Ubuntu uses /var/log/auth.log, Fedora/RHEL uses /var/log/secure.
		for _, logPath := range []string{"/var/log/auth.log", "/var/log/secure"} {
			if out, err := exec.CommandContext(ctx, "grep", "-c", "Failed password", logPath).Output(); err == nil {
				if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
					info.FailedLogins24h = n
				}
				break
			}
		}
	}

	// Per-interval auth-failure count (FR-001, AD-001).
	// Drain the shared counter populated by LogsCollector's parseAndCount.
	// If IsReady() is false (first interval or LogsCollector never ran), report
	// zero to satisfy FR-005 (no historical backfill on first push).
	n := c.authCounter.Drain()
	if c.authCounter.IsReady() {
		info.FailedLoginsInterval = &n
	} else {
		zero := 0
		info.FailedLoginsInterval = &zero
	}

	return info, nil
}

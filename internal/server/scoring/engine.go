package scoring

import (
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// ScoreOptions provides extra device-level context for scoring.
type ScoreOptions struct {
	AutoPatch bool // rIOt-managed automatic OS patching enabled
}

// Score evaluates the security posture of a device from its telemetry.
// Categories with no telemetry data are omitted from scoring.
func Score(data *models.FullTelemetryData, opts ...ScoreOptions) *models.SecurityScore {
	var opt ScoreOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	var categories []models.SecurityCategoryScore

	if cat := accessControl(data.Security); cat != nil {
		categories = append(categories, *cat)
	}
	if cat := patching(data.Updates, opt.AutoPatch); cat != nil {
		categories = append(categories, *cat)
	}
	if cat := network(data.Security, data.WebServers); cat != nil {
		categories = append(categories, *cat)
	}
	if cat := docker(data.Docker); cat != nil {
		categories = append(categories, *cat)
	}
	if cat := system(data.Services, data.OS, data.Network); cat != nil {
		categories = append(categories, *cat)
	}

	earned, max := 0, 0
	for _, c := range categories {
		earned += c.Score
		max += c.MaxScore
	}

	overall := 0
	if max > 0 {
		overall = earned * 100 / max
	}

	return &models.SecurityScore{
		OverallScore: overall,
		MaxScore:     100,
		Grade:        grade(overall),
		Categories:   categories,
		EvaluatedAt:  time.Now().UTC(),
	}
}

func grade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 60:
		return "C"
	case score >= 40:
		return "D"
	default:
		return "F"
	}
}

// ── Access Control ───────────────────────────────────────────────────────────

func accessControl(sec *models.SecurityInfo) *models.SecurityCategoryScore {
	if sec == nil {
		return nil
	}
	cat := &models.SecurityCategoryScore{
		Category: models.CategoryAccessControl,
		Label:    "Access Control",
	}

	// Firewall active
	fwActive := sec.FirewallStatus == "active" || sec.FirewallStatus == "enabled"
	cat.Findings = append(cat.Findings, models.SecurityFinding{
		ID:          "fw-active",
		Category:    models.CategoryAccessControl,
		Title:       "Firewall enabled",
		Description: descIf(fwActive, "Firewall is active", "No active firewall detected (status: "+sec.FirewallStatus+")"),
		Remediation: "Enable UFW or iptables: sudo ufw enable",
		RefURL:      "https://wiki.ubuntu.com/UncomplicatedFirewall",
		Severity:    severityIf(fwActive, models.SecSeverityCritical),
		Weight:      8,
		Passed:      fwActive,
	})

	// Mandatory access control (SELinux or AppArmor)
	macEnabled := sec.SELinux == "enforcing" ||
		sec.AppArmor == "enabled" || sec.AppArmor == "active"
	macDesc := "No mandatory access control detected"
	if sec.SELinux == "enforcing" {
		macDesc = "SELinux is enforcing"
	} else if sec.AppArmor == "enabled" || sec.AppArmor == "active" {
		macDesc = "AppArmor is enabled"
	}
	cat.Findings = append(cat.Findings, models.SecurityFinding{
		ID:          "mac-enabled",
		Category:    models.CategoryAccessControl,
		Title:       "Mandatory access control",
		Description: macDesc,
		Remediation: "Enable AppArmor or SELinux for process sandboxing",
		RefURL:      "https://www.redhat.com/en/topics/linux/what-is-selinux",
		Severity:    severityIf(macEnabled, models.SecSeverityWarning),
		Weight:      5,
		Passed:      macEnabled,
	})

	// Failed logins
	lowFailed := sec.FailedLogins24h < 10
	cat.Findings = append(cat.Findings, models.SecurityFinding{
		ID:       "failed-logins-low",
		Category: models.CategoryAccessControl,
		Title:    "Failed login attempts",
		Description: descIf(lowFailed,
			"Low failed login count in last 24h",
			fmtf("%d failed login attempts in last 24h", sec.FailedLogins24h)),
		Remediation: "Investigate failed logins; consider fail2ban or SSH key-only auth",
		RefURL:      "https://www.fail2ban.org/",
		Severity:    ternary(lowFailed, models.SecSeverityPass, ternary(sec.FailedLogins24h >= 50, models.SecSeverityCritical, models.SecSeverityWarning)),
		Weight:      5,
		Passed:      lowFailed,
	})

	// Logged-in users
	fewUsers := sec.LoggedInUsers <= 2
	cat.Findings = append(cat.Findings, models.SecurityFinding{
		ID:       "logged-in-users",
		Category: models.CategoryAccessControl,
		Title:    "Active user sessions",
		Description: descIf(fewUsers,
			fmtf("%d active session(s)", sec.LoggedInUsers),
			fmtf("%d active sessions — more than expected", sec.LoggedInUsers)),
		Remediation: "Review active sessions with 'who' and terminate unexpected ones",
		RefURL:      "https://www.cyberciti.biz/faq/unix-linux-who-command-examples-syntax-usage/",
		Severity:    severityIf(fewUsers, models.SecSeverityWarning),
		Weight:      4,
		Passed:      fewUsers,
	})

	tally(cat)
	return cat
}

// ── Patching ─────────────────────────────────────────────────────────────────

func patching(upd *models.UpdateInfo, autoPatch bool) *models.SecurityCategoryScore {
	if upd == nil {
		return nil
	}
	cat := &models.SecurityCategoryScore{
		Category: models.CategoryPatching,
		Label:    "Patching",
	}

	// Security updates
	noSec := upd.PendingSecurityCount == 0
	cat.Findings = append(cat.Findings, models.SecurityFinding{
		ID:       "no-security-updates",
		Category: models.CategoryPatching,
		Title:    "Pending security updates",
		Description: descIf(noSec,
			"No pending security updates",
			fmtf("%d pending security update(s)", upd.PendingSecurityCount)),
		Remediation: "Apply security updates: sudo apt upgrade or equivalent",
		RefURL:      "https://wiki.debian.org/SecurityManagement",
		Severity:    severityIf(noSec, models.SecSeverityCritical),
		Weight:      10,
		Passed:      noSec,
	})

	// General updates
	fewUpdates := upd.PendingUpdates <= 5
	cat.Findings = append(cat.Findings, models.SecurityFinding{
		ID:       "pending-updates-low",
		Category: models.CategoryPatching,
		Title:    "Pending package updates",
		Description: descIf(fewUpdates,
			fmtf("%d pending update(s)", upd.PendingUpdates),
			fmtf("%d pending updates — system is behind", upd.PendingUpdates)),
		Remediation: "Run system updates regularly to stay current",
		RefURL:      "https://wiki.debian.org/AptCLI",
		Severity:    severityIf(fewUpdates, models.SecSeverityWarning),
		Weight:      5,
		Passed:      fewUpdates,
	})

	// Kernel update
	noKernel := !upd.PendingKernelUpdate
	kernelDesc := "Kernel is up to date"
	if upd.PendingKernelUpdate {
		kernelDesc = "Kernel update available"
		if upd.PendingKernelVersion != "" {
			kernelDesc += " (" + upd.PendingKernelVersion + ")"
		}
	}
	cat.Findings = append(cat.Findings, models.SecurityFinding{
		ID:          "no-kernel-update",
		Category:    models.CategoryPatching,
		Title:       "Kernel update pending",
		Description: kernelDesc,
		Remediation: "Apply kernel update and reboot to activate",
		RefURL:      "https://wiki.ubuntu.com/Kernel/LTSEnablementStack",
		Severity:    severityIf(noKernel, models.SecSeverityWarning),
		Weight:      5,
		Passed:      noKernel,
	})

	// Unattended upgrades — pass if OS-level unattended-upgrades OR rIOt auto-patch is enabled
	hasAutoUpdates := upd.UnattendedUpgrades || autoPatch
	autoDesc := "Automatic updates not configured"
	if upd.UnattendedUpgrades && autoPatch {
		autoDesc = "Unattended upgrades enabled + rIOt auto-patch enabled"
	} else if autoPatch {
		autoDesc = "rIOt auto-patch enabled"
	} else if upd.UnattendedUpgrades {
		autoDesc = "Unattended upgrades enabled"
	}
	cat.Findings = append(cat.Findings, models.SecurityFinding{
		ID:          "auto-updates",
		Category:    models.CategoryPatching,
		Title:       "Automatic security updates",
		Description: autoDesc,
		Remediation: "Enable rIOt auto-patch or unattended-upgrades for automatic security patches",
		RefURL:      "https://wiki.debian.org/UnattendedUpgrades",
		Severity:    severityIf(hasAutoUpdates, models.SecSeverityInfo),
		Weight:      5,
		Passed:      hasAutoUpdates,
	})

	tally(cat)
	return cat
}

// ── Network ──────────────────────────────────────────────────────────────────

func network(sec *models.SecurityInfo, ws *models.WebServerInfo) *models.SecurityCategoryScore {
	if sec == nil && ws == nil {
		return nil
	}
	cat := &models.SecurityCategoryScore{
		Category: models.CategoryNetwork,
		Label:    "Network",
	}

	if sec != nil {
		// Open ports count
		fewPorts := len(sec.OpenPorts) <= 5
		cat.Findings = append(cat.Findings, models.SecurityFinding{
			ID:       "minimal-open-ports",
			Category: models.CategoryNetwork,
			Title:    "Open port count",
			Description: descIf(fewPorts,
				fmtf("%d open port(s)", len(sec.OpenPorts)),
				fmtf("%d open ports — review for unnecessary services", len(sec.OpenPorts))),
			Remediation: "Close unused ports and services to reduce attack surface",
			RefURL:      "https://www.cisecurity.org/benchmark/distribution_independent_linux",
			Severity:    severityIf(fewPorts, models.SecSeverityWarning),
			Weight:      5,
			Passed:      fewPorts,
		})

		// Risky ports
		riskyPorts := map[int]string{21: "FTP", 23: "Telnet", 25: "SMTP", 514: "rsh"}
		var found []string
		for _, p := range sec.OpenPorts {
			if name, ok := riskyPorts[p]; ok {
				found = append(found, fmtf("%s (%d)", name, p))
			}
		}
		noRisky := len(found) == 0
		cat.Findings = append(cat.Findings, models.SecurityFinding{
			ID:       "no-risky-ports",
			Category: models.CategoryNetwork,
			Title:    "Insecure service ports",
			Description: descIf(noRisky,
				"No insecure service ports open",
				"Insecure ports open: "+strings.Join(found, ", ")),
			Remediation: "Disable legacy protocols (FTP, Telnet) and use secure alternatives",
			RefURL:      "https://www.cisecurity.org/benchmark/distribution_independent_linux",
			Severity:    severityIf(noRisky, models.SecSeverityCritical),
			Weight:      5,
			Passed:      noRisky,
		})
	}

	if ws != nil {
		// TLS certificate validity
		allCertsOK := true
		var certIssues []string
		for _, srv := range ws.Servers {
			for _, cert := range srv.Certs {
				if cert.IsCA {
					continue
				}
				if cert.DaysLeft <= 0 {
					allCertsOK = false
					certIssues = append(certIssues, cert.Subject+" (EXPIRED)")
				} else if cert.DaysLeft <= 30 {
					allCertsOK = false
					certIssues = append(certIssues, fmtf("%s (%dd left)", cert.Subject, cert.DaysLeft))
				}
			}
		}
		cat.Findings = append(cat.Findings, models.SecurityFinding{
			ID:       "tls-certs-valid",
			Category: models.CategoryNetwork,
			Title:    "TLS certificate status",
			Description: descIf(allCertsOK,
				"All TLS certificates valid (>30 days)",
				"Certificate issues: "+strings.Join(certIssues, "; ")),
			Remediation: "Renew expiring certificates; consider automated renewal with certbot",
			RefURL:      "https://certbot.eff.org/",
			Severity:    severityIf(allCertsOK, models.SecSeverityCritical),
			Weight:      5,
			Passed:      allCertsOK,
		})

		// Web server config valid
		allValid := true
		for _, srv := range ws.Servers {
			if srv.ConfigValid != nil && !*srv.ConfigValid {
				allValid = false
			}
		}
		cat.Findings = append(cat.Findings, models.SecurityFinding{
			ID:          "proxy-config-valid",
			Category:    models.CategoryNetwork,
			Title:       "Web server configuration",
			Description: descIf(allValid, "All web server configs are valid", "One or more web server configs have errors"),
			Remediation: "Fix configuration errors shown in web server details",
			RefURL:      "https://nginx.org/en/docs/beginners_guide.html",
			Severity:    severityIf(allValid, models.SecSeverityWarning),
			Weight:      3,
			Passed:      allValid,
		})

		// Security headers
		hasHeaders := false
		for _, srv := range ws.Servers {
			if srv.SecurityConfig != nil && len(srv.SecurityConfig.SecurityHeaders) >= 3 {
				hasHeaders = true
				break
			}
		}
		cat.Findings = append(cat.Findings, models.SecurityFinding{
			ID:          "security-headers",
			Category:    models.CategoryNetwork,
			Title:       "Security headers configured",
			Description: descIf(hasHeaders, "Security headers present (HSTS, X-Frame-Options, etc.)", "Fewer than 3 security headers configured"),
			Remediation: "Add HSTS, X-Frame-Options, X-Content-Type-Options, and CSP headers",
			RefURL:      "https://owasp.org/www-project-secure-headers/",
			Severity:    severityIf(hasHeaders, models.SecSeverityInfo),
			Weight:      4,
			Passed:      hasHeaders,
		})

		// Rate limiting
		hasRL := false
		for _, srv := range ws.Servers {
			if srv.SecurityConfig != nil && len(srv.SecurityConfig.RateLimiting) > 0 {
				hasRL = true
				break
			}
		}
		cat.Findings = append(cat.Findings, models.SecurityFinding{
			ID:          "rate-limiting",
			Category:    models.CategoryNetwork,
			Title:       "Rate limiting configured",
			Description: descIf(hasRL, "Rate limiting is configured", "No rate limiting detected in web server config"),
			Remediation: "Add rate limiting to protect against brute force and abuse",
			RefURL:      "https://www.nginx.com/blog/rate-limiting-nginx/",
			Severity:    severityIf(hasRL, models.SecSeverityInfo),
			Weight:      3,
			Passed:      hasRL,
		})
	}

	tally(cat)
	return cat
}

// ── Docker ───────────────────────────────────────────────────────────────────

func docker(d *models.DockerInfo) *models.SecurityCategoryScore {
	if d == nil || !d.Available {
		return nil
	}

	running := runningContainers(d)
	if len(running) == 0 {
		return nil
	}

	cat := &models.SecurityCategoryScore{
		Category: models.CategoryDocker,
		Label:    "Docker",
	}

	// Restart policies
	allRestart := true
	for _, c := range running {
		if c.RestartPolicy == "" || c.RestartPolicy == "no" {
			allRestart = false
			break
		}
	}
	cat.Findings = append(cat.Findings, models.SecurityFinding{
		ID:          "container-restart-policy",
		Category:    models.CategoryDocker,
		Title:       "Container restart policies",
		Description: descIf(allRestart, "All running containers have restart policies", "Some containers lack a restart policy"),
		Remediation: "Set restart policy to 'unless-stopped' or 'on-failure' for all containers",
		RefURL:      "https://docs.docker.com/engine/containers/start-containers-automatically/",
		Severity:    severityIf(allRestart, models.SecSeverityWarning),
		Weight:      4,
		Passed:      allRestart,
	})

	// Health checks
	hasHealth := true
	for _, c := range running {
		if c.HealthStatus == "" || c.HealthStatus == "none" {
			hasHealth = false
			break
		}
	}
	cat.Findings = append(cat.Findings, models.SecurityFinding{
		ID:          "container-health-checks",
		Category:    models.CategoryDocker,
		Title:       "Container health checks",
		Description: descIf(hasHealth, "All running containers have health checks", "Some containers lack health checks"),
		Remediation: "Add HEALTHCHECK to Dockerfiles or docker-compose health check config",
		RefURL:      "https://docs.docker.com/reference/dockerfile/#healthcheck",
		Severity:    severityIf(hasHealth, models.SecSeverityInfo),
		Weight:      3,
		Passed:      hasHealth,
	})

	// Memory limits
	allMemLimits := true
	for _, c := range running {
		if c.MemLimit <= 0 {
			allMemLimits = false
			break
		}
	}
	cat.Findings = append(cat.Findings, models.SecurityFinding{
		ID:          "container-mem-limits",
		Category:    models.CategoryDocker,
		Title:       "Container memory limits",
		Description: descIf(allMemLimits, "All running containers have memory limits", "Some containers have no memory limit set"),
		Remediation: "Set memory limits in docker-compose or with --memory flag to prevent OOM issues",
		RefURL:      "https://docs.docker.com/engine/containers/resource_constraints/",
		Severity:    severityIf(allMemLimits, models.SecSeverityWarning),
		Weight:      4,
		Passed:      allMemLimits,
	})

	// Sensitive bind mounts
	sensitivePaths := []string{"/var/run/docker.sock"}
	hasSensitive := false
	for _, c := range running {
		for _, m := range c.Mounts {
			if m.Type == "bind" && !m.ReadOnly {
				for _, sp := range sensitivePaths {
					if m.Source == sp {
						hasSensitive = true
					}
				}
			}
		}
	}
	noSensitive := !hasSensitive
	cat.Findings = append(cat.Findings, models.SecurityFinding{
		ID:          "no-sensitive-mounts",
		Category:    models.CategoryDocker,
		Title:       "Sensitive volume mounts",
		Description: descIf(noSensitive, "No writable sensitive mounts detected", "Docker socket or other sensitive paths mounted read-write"),
		Remediation: "Mount docker.sock read-only (:ro) or use a Docker socket proxy",
		RefURL:      "https://docs.docker.com/engine/security/#docker-daemon-attack-surface",
		Severity:    severityIf(noSensitive, models.SecSeverityWarning),
		Weight:      4,
		Passed:      noSensitive,
	})

	tally(cat)
	return cat
}

// ── System ───────────────────────────────────────────────────────────────────

func system(services []models.ServiceInfo, os *models.OSInfo, net *models.NetworkInfo) *models.SecurityCategoryScore {
	if len(services) == 0 && os == nil && net == nil {
		return nil
	}
	cat := &models.SecurityCategoryScore{
		Category: models.CategorySystem,
		Label:    "System",
	}

	if len(services) > 0 {
		failedCount := 0
		for _, s := range services {
			if s.State == "failed" {
				failedCount++
			}
		}
		noFailed := failedCount == 0
		cat.Findings = append(cat.Findings, models.SecurityFinding{
			ID:       "no-failed-services",
			Category: models.CategorySystem,
			Title:    "Failed systemd services",
			Description: descIf(noFailed,
				"No failed services",
				fmtf("%d failed service(s) detected", failedCount)),
			Remediation: "Investigate and fix failed services with 'systemctl status <service>'",
			RefURL:      "https://www.freedesktop.org/software/systemd/man/latest/systemctl.html",
			Severity:    severityIf(noFailed, models.SecSeverityWarning),
			Weight:      5,
			Passed:      noFailed,
		})
	}

	if os != nil {
		// Uptime check — very long uptime means missing kernel patches
		const sixMonths = 180 * 24 * 3600
		reasonable := os.Uptime < sixMonths
		cat.Findings = append(cat.Findings, models.SecurityFinding{
			ID:       "reasonable-uptime",
			Category: models.CategorySystem,
			Title:    "System uptime",
			Description: descIf(reasonable,
				fmtf("Uptime: %d days", os.Uptime/86400),
				fmtf("Uptime: %d days — system may be missing kernel patches that require a reboot", os.Uptime/86400)),
			Remediation: "Reboot to apply pending kernel updates",
			RefURL:      "https://www.cyberciti.biz/tips/linux-last-reboot-time-and-date-find-out.html",
			Severity:    severityIf(reasonable, models.SecSeverityInfo),
			Weight:      3,
			Passed:      reasonable,
		})
	}

	if net != nil {
		hasDNS := len(net.DNSServers) > 0
		cat.Findings = append(cat.Findings, models.SecurityFinding{
			ID:          "dns-configured",
			Category:    models.CategorySystem,
			Title:       "DNS configured",
			Description: descIf(hasDNS, fmtf("DNS servers: %s", strings.Join(net.DNSServers, ", ")), "No DNS servers configured"),
			Remediation: "Configure DNS servers in /etc/resolv.conf or systemd-resolved",
			RefURL:      "https://www.freedesktop.org/software/systemd/man/latest/systemd-resolved.service.html",
			Severity:    severityIf(hasDNS, models.SecSeverityWarning),
			Weight:      2,
			Passed:      hasDNS,
		})
	}

	tally(cat)
	return cat
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func tally(cat *models.SecurityCategoryScore) {
	for _, f := range cat.Findings {
		cat.MaxScore += f.Weight
		if f.Passed {
			cat.Score += f.Weight
		}
	}
}

func runningContainers(d *models.DockerInfo) []models.ContainerInfo {
	var out []models.ContainerInfo
	for _, c := range d.Containers {
		if c.State == "running" {
			out = append(out, c)
		}
	}
	return out
}

func severityIf(passed bool, failSeverity models.SecuritySeverity) models.SecuritySeverity {
	if passed {
		return models.SecSeverityPass
	}
	return failSeverity
}

func descIf(passed bool, passDesc, failDesc string) string {
	if passed {
		return passDesc
	}
	return failDesc
}

func ternary(cond bool, a, b models.SecuritySeverity) models.SecuritySeverity {
	if cond {
		return a
	}
	return b
}

func fmtf(format string, args ...interface{}) string {
	return strings.NewReplacer().Replace(sprintf(format, args...))
}

func sprintf(format string, args ...interface{}) string {
	// Minimal sprintf without importing fmt to keep binary small.
	// For this use case we only need %d and %s.
	result := format
	for _, arg := range args {
		switch v := arg.(type) {
		case int:
			result = strings.Replace(result, "%d", itoa(v), 1)
		case int64:
			result = strings.Replace(result, "%d", itoa(int(v)), 1)
		case uint64:
			result = strings.Replace(result, "%d", uitoa(v), 1)
		case string:
			result = strings.Replace(result, "%s", v, 1)
		}
	}
	return result
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	s := uitoa(uint64(n))
	if neg {
		return "-" + s
	}
	return s
}

func uitoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

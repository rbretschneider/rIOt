package collectors

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// FerronParser detects and parses Ferron web server configuration.
// Ferron uses KDL-format config files (ferron.kdl).
type FerronParser struct{}

func (p *FerronParser) Name() string { return "ferron" }

func (p *FerronParser) Detect(ctx context.Context) *models.ProxyServer {
	path, err := lookBinary("ferron", "/usr/bin/ferron", "/usr/local/bin/ferron", "/usr/sbin/ferron")
	if err != nil {
		return nil
	}

	srv := &models.ProxyServer{
		Name:   "ferron",
		Status: "unknown",
	}

	// Version: ferron --version outputs e.g. "ferron 2.6.0" or "Ferron 2.6.0"
	if out, err := exec.CommandContext(ctx, path, "--version").Output(); err == nil {
		v := strings.TrimSpace(string(out))
		if fields := strings.Fields(v); len(fields) >= 2 {
			srv.Version = fields[1]
		} else if len(fields) == 1 {
			srv.Version = fields[0]
		}
	}

	// Status via systemctl
	if out, err := exec.CommandContext(ctx, "systemctl", "is-active", "ferron").Output(); err == nil {
		state := strings.TrimSpace(string(out))
		if state == "active" {
			srv.Status = "running"
		} else {
			srv.Status = state
		}
	}

	// PID
	if out, err := exec.CommandContext(ctx, "systemctl", "show", "ferron", "--property=MainPID", "--value").Output(); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && pid > 0 {
			srv.PID = pid
		}
	}

	// Config path — check common locations
	for _, cp := range []string{"/etc/ferron/ferron.kdl", "/etc/ferron.kdl"} {
		if _, err := os.Stat(cp); err == nil {
			srv.ConfigPath = cp
			break
		}
	}

	return srv
}

func (p *FerronParser) Parse(ctx context.Context, server *models.ProxyServer) error {
	if server.ConfigPath == "" {
		return fmt.Errorf("no config file found")
	}

	content, err := p.loadConfig(server.ConfigPath)
	if err != nil {
		return err
	}

	sites, upstreams, sec := parseFerronConfig(content)
	server.Sites = sites
	server.Upstreams = upstreams
	server.SecurityConfig = sec

	// Scan certs from TLS directives and auto_tls cache
	p.scanCerts(server, content)

	return nil
}

// loadConfig reads the main config file and recursively processes include directives.
func (p *FerronParser) loadConfig(path string) (string, error) {
	absPath, _ := filepath.Abs(path)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	content := string(data)
	var result strings.Builder
	result.WriteString(content)

	// Process include directives
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "include ") {
			pattern := strings.TrimSpace(strings.TrimPrefix(trimmed, "include"))
			pattern = strings.Trim(pattern, "\"")
			matches, err := filepath.Glob(pattern)
			if err != nil {
				continue
			}
			for _, m := range matches {
				mAbs, _ := filepath.Abs(m)
				if mAbs == absPath {
					continue // skip the main config file itself
				}
				inc, err := os.ReadFile(m)
				if err != nil {
					continue
				}
				result.WriteString("\n")
				result.WriteString(string(inc))
			}
		}
	}

	return result.String(), nil
}

// parseFerronConfig parses a Ferron KDL config and extracts sites, upstreams, and security config.
func parseFerronConfig(content string) ([]models.ProxySite, []models.ProxyUpstream, *models.ProxySecurityCfg) {
	lines := strings.Split(content, "\n")
	var sites []models.ProxySite
	upstreamMap := make(map[string]*models.ProxyUpstream)
	sec := &models.ProxySecurityCfg{
		SecurityHeaders: make(map[string]string),
	}

	var currentSite *models.ProxySite
	depth := 0
	siteDepth := 0 // depth at which the current site block started

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		openBraces := strings.Count(trimmed, "{")
		closeBraces := strings.Count(trimmed, "}")

		// Top-level block: could be a virtual host, "globals", or "snippet"
		if depth == 0 && openBraces > 0 {
			addr := strings.TrimSpace(strings.Split(trimmed, "{")[0])
			addr = strings.Trim(addr, "\"")

			// Skip non-site blocks
			if addr == "globals" || addr == "*" || strings.HasPrefix(addr, "snippet ") {
				depth += openBraces - closeBraces
				continue
			}

			currentSite = &models.ProxySite{
				Enabled: true,
			}
			siteDepth = 1

			// Parse address — could be "example.com", "example.com:8080",
			// or "example.com,example.org" (comma-separated, no spaces)
			for _, a := range strings.Fields(addr) {
				for _, host := range strings.Split(a, ",") {
					host = strings.TrimPrefix(host, "http://")
					host = strings.TrimPrefix(host, "https://")
					if strings.HasPrefix(host, ":") {
						currentSite.Listen = append(currentSite.Listen, host)
					} else if strings.Contains(host, ":") {
						// host:port
						parts := strings.SplitN(host, ":", 2)
						currentSite.ServerNames = append(currentSite.ServerNames, parts[0])
						currentSite.Listen = append(currentSite.Listen, parts[1])
					} else {
						currentSite.ServerNames = append(currentSite.ServerNames, host)
					}
				}
			}

			depth += openBraces - closeBraces
			continue
		}

		depth += openBraces - closeBraces

		if currentSite != nil && depth >= siteDepth {
			parseFerronDirective(trimmed, currentSite, upstreamMap, sec)
		}

		if depth <= 0 && currentSite != nil {
			if len(currentSite.ServerNames) > 0 || currentSite.ProxyPass != "" || currentSite.Root != "" {
				sites = append(sites, *currentSite)
			}
			currentSite = nil
			depth = 0
		}
	}

	// Convert upstream map to slice
	var upstreams []models.ProxyUpstream
	for _, u := range upstreamMap {
		upstreams = append(upstreams, *u)
	}

	// Only return security config if there's something
	if len(sec.RateLimiting) == 0 && len(sec.AccessControls) == 0 && len(sec.SecurityHeaders) == 0 {
		sec = nil
	}

	return sites, upstreams, sec
}

// parseFerronDirective processes a single line inside a Ferron site block.
func parseFerronDirective(trimmed string, site *models.ProxySite, upstreamMap map[string]*models.ProxyUpstream, sec *models.ProxySecurityCfg) {
	fields := strings.Fields(trimmed)
	if len(fields) < 2 {
		return
	}
	directive := fields[0]
	value := strings.Trim(fields[1], "\"")

	switch directive {
	case "root":
		site.Root = value

	case "proxy":
		site.ProxyPass = value
		// Build upstream entry
		upName := fmt.Sprintf("upstream_%d", len(upstreamMap))
		if len(site.ServerNames) > 0 {
			upName = site.ServerNames[0]
		}
		if _, ok := upstreamMap[upName]; !ok {
			upstreamMap[upName] = &models.ProxyUpstream{Name: upName}
		}
		upstreamMap[upName].Servers = append(upstreamMap[upName].Servers, models.UpstreamServer{
			Address: value,
		})

	case "proxy_srv":
		site.ProxyPass = value

	case "tls":
		// tls <cert_path> <key_path>
		site.SSLCert = value

	case "header":
		if len(fields) >= 3 {
			headerName := strings.Trim(fields[1], "\"")
			headerVal := strings.Trim(strings.Join(fields[2:], " "), "\"")
			secHeaders := map[string]bool{
				"Strict-Transport-Security":  true,
				"X-Frame-Options":            true,
				"X-Content-Type-Options":     true,
				"X-XSS-Protection":           true,
				"Content-Security-Policy":    true,
				"Referrer-Policy":            true,
				"Permissions-Policy":         true,
				"Cross-Origin-Opener-Policy": true,
			}
			if secHeaders[headerName] {
				sec.SecurityHeaders[headerName] = headerVal
			}
		}

	case "limit":
		rule := models.RateLimitRule{Zone: "default"}
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "rate=") {
				rule.Rate = strings.TrimPrefix(f, "rate=") + "r/s"
			}
			if strings.HasPrefix(f, "burst=") {
				if b, err := strconv.Atoi(strings.TrimPrefix(f, "burst=")); err == nil {
					rule.Burst = b
				}
			}
		}
		if rule.Rate != "" {
			sec.RateLimiting = append(sec.RateLimiting, rule)
		}

	case "block":
		for _, addr := range fields[1:] {
			addr = strings.Trim(addr, "\"")
			sec.AccessControls = append(sec.AccessControls, models.AccessRule{
				Directive: "deny",
				Value:     addr,
			})
		}

	case "allow":
		for _, addr := range fields[1:] {
			addr = strings.Trim(addr, "\"")
			sec.AccessControls = append(sec.AccessControls, models.AccessRule{
				Directive: "allow",
				Value:     addr,
			})
		}
	}
}

// scanCerts looks for TLS certificates referenced in the config and in auto_tls cache directories.
func (p *FerronParser) scanCerts(server *models.ProxyServer, content string) {
	// Collect cert paths from tls directives
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && fields[0] == "tls" {
			certPath := strings.Trim(fields[1], "\"")
			cert, err := parseCertFile(certPath)
			if err == nil {
				server.Certs = append(server.Certs, *cert)
			}
		}
	}

	// Scan auto_tls cache directories
	cacheDirs := []string{
		"/var/lib/ferron/certs",
		"/var/lib/ferron/.certificates",
	}

	// Check if config specifies a custom auto_tls_cache
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && fields[0] == "auto_tls_cache" {
			dir := strings.Trim(fields[1], "\"")
			cacheDirs = append([]string{dir}, cacheDirs...)
		}
	}

	for _, dir := range cacheDirs {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(info.Name(), ".crt") || strings.HasSuffix(info.Name(), ".pem") {
				cert, err := parseCertFile(path)
				if err == nil {
					server.Certs = append(server.Certs, *cert)
				}
			}
			return nil
		})
		break // Only scan first found directory
	}
}

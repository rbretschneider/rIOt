package collectors

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// NginxParser detects and parses nginx configuration.
type NginxParser struct{}

func (p *NginxParser) Name() string { return "nginx" }

func (p *NginxParser) Detect(ctx context.Context) *models.ProxyServer {
	path, err := lookBinary("nginx", "/usr/sbin/nginx", "/usr/local/sbin/nginx", "/usr/bin/nginx", "/usr/local/bin/nginx")
	if err != nil {
		return nil
	}

	srv := &models.ProxyServer{
		Name:   "nginx",
		Status: "unknown",
	}

	// Version — nginx -v doesn't need elevated privileges
	if out, err := exec.CommandContext(ctx, path, "-v").CombinedOutput(); err == nil {
		// nginx -v writes to stderr: "nginx version: nginx/1.24.0"
		s := strings.TrimSpace(string(out))
		if i := strings.Index(s, "nginx/"); i >= 0 {
			srv.Version = s[i+len("nginx/"):]
		}
	}

	// Status via systemctl
	if out, err := exec.CommandContext(ctx, "systemctl", "is-active", "nginx").Output(); err == nil {
		state := strings.TrimSpace(string(out))
		if state == "active" {
			srv.Status = "running"
		} else {
			srv.Status = state
		}
	}

	// PID
	if out, err := exec.CommandContext(ctx, "systemctl", "show", "nginx", "--property=MainPID", "--value").Output(); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && pid > 0 {
			srv.PID = pid
		}
	}

	// Config path + validation via sudo nginx -t.
	// Requires sudoers rule for the agent user.
	if out, err := exec.CommandContext(ctx, "sudo", path, "-t").CombinedOutput(); err == nil {
		s := string(out)
		if i := strings.Index(s, "configuration file "); i >= 0 {
			rest := s[i+len("configuration file "):]
			if j := strings.IndexAny(rest, " \n"); j >= 0 {
				srv.ConfigPath = rest[:j]
			}
		}
		valid := true
		srv.ConfigValid = &valid
	} else {
		valid := false
		srv.ConfigValid = &valid
		s := string(out)
		srv.ConfigError = strings.TrimSpace(s)
		// Still extract config path from error output
		if i := strings.Index(s, "configuration file "); i >= 0 {
			rest := s[i+len("configuration file "):]
			if j := strings.IndexAny(rest, " \n"); j >= 0 {
				srv.ConfigPath = rest[:j]
			}
		}
	}

	return srv
}

func (p *NginxParser) Parse(ctx context.Context, server *models.ProxyServer) error {
	path, err := lookBinary("nginx", "/usr/sbin/nginx", "/usr/local/sbin/nginx", "/usr/bin/nginx", "/usr/local/bin/nginx")
	if err != nil {
		return nil
	}

	// nginx -T dumps the full resolved configuration. Uses sudo so the agent
	// can read SSL cert files owned by root (e.g. /etc/letsencrypt/live/).
	out, _ := exec.CommandContext(ctx, "sudo", path, "-T").CombinedOutput()
	config := string(out)

	// Check if nginx -T produced actual config content (not just error messages).
	// Real config output contains "# configuration file" markers.
	if !strings.Contains(config, "# configuration file ") {
		// Fallback: read config files directly from disk.
		slog.Debug("nginx -T produced no config, falling back to disk read", "output_len", len(config))
		config = p.readConfigFromDisk(server.ConfigPath)
	}

	if len(config) == 0 {
		return nil
	}

	server.Sites = parseNginxSites(config)
	server.Upstreams = parseNginxUpstreams(config)
	server.SecurityConfig = parseNginxSecurity(config)

	// Parse certificates referenced in sites
	certPaths := make(map[string]bool)
	for _, site := range server.Sites {
		if site.SSLCert != "" {
			certPaths[site.SSLCert] = true
		}
	}
	for certPath := range certPaths {
		cert, err := parseCertFile(certPath)
		if err != nil {
			slog.Debug("failed to parse certificate", "path", certPath, "error", err)
			continue
		}
		server.Certs = append(server.Certs, *cert)
	}

	return nil
}

// readConfigFromDisk reads nginx config files directly from disk as a fallback
// when nginx -T is unavailable (e.g. agent lacks permission to load SSL certs).
// It produces output in the same format as nginx -T: each file prefixed with
// "# configuration file <path>:" so existing parsers work unchanged.
func (p *NginxParser) readConfigFromDisk(mainConfig string) string {
	if mainConfig == "" {
		// Try common default locations
		for _, path := range []string{"/etc/nginx/nginx.conf", "/usr/local/etc/nginx/nginx.conf"} {
			if _, err := os.Stat(path); err == nil {
				mainConfig = path
				break
			}
		}
	}
	if mainConfig == "" {
		return ""
	}

	visited := make(map[string]bool)
	var buf strings.Builder
	p.readConfigFile(mainConfig, visited, &buf)
	return buf.String()
}

// readConfigFile recursively reads a config file and follows include directives.
// Includes are inlined at the point of the directive (matching nginx -T behavior)
// so that server blocks remain inside their parent http {} context.
func (p *NginxParser) readConfigFile(path string, visited map[string]bool, buf *strings.Builder) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return
	}
	if visited[absPath] {
		return
	}
	visited[absPath] = true

	data, err := os.ReadFile(absPath)
	if err != nil {
		slog.Debug("failed to read nginx config file", "path", absPath, "error", err)
		return
	}

	// Write the marker that nginx -T would produce
	buf.WriteString("# configuration file ")
	buf.WriteString(absPath)
	buf.WriteString(":\n")

	// Process line by line, inlining includes where they appear
	baseDir := filepath.Dir(absPath)
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		stripped := strings.TrimSuffix(trimmed, ";")

		if strings.HasPrefix(stripped, "include ") {
			pattern := strings.TrimSpace(strings.TrimPrefix(stripped, "include"))
			if pattern == "" {
				buf.WriteString(line)
				buf.WriteString("\n")
				continue
			}

			// Resolve relative paths against the config file's directory
			if !filepath.IsAbs(pattern) {
				pattern = filepath.Join(baseDir, pattern)
			}

			// Glob expand and inline each matched file
			matches, err := filepath.Glob(pattern)
			if err != nil {
				slog.Debug("failed to glob nginx include", "pattern", pattern, "error", err)
				buf.WriteString(line)
				buf.WriteString("\n")
				continue
			}

			for _, match := range matches {
				info, err := os.Stat(match)
				if err != nil || info.IsDir() {
					continue
				}
				p.readConfigFile(match, visited, buf)
			}
		} else {
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}
}

// parseNginxSites extracts server blocks from nginx -T output.
// The output includes "# configuration file <path>:" markers before each included file.
func parseNginxSites(config string) []models.ProxySite {
	var sites []models.ProxySite

	// Track current configuration file from "# configuration file <path>:" markers
	currentFile := ""
	lines := strings.Split(config, "\n")

	// Simple state machine: track brace depth to find server blocks
	type serverBlock struct {
		configFile string
		lines      []string
		depth      int
	}

	var current *serverBlock
	httpDepth := 0
	inHTTP := false
	seenHTTP := false // stays true after first http block — catches server blocks from included files listed separately in nginx -T output
	pendingHTTP := false
	pendingServer := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track configuration file markers
		if strings.HasPrefix(trimmed, "# configuration file ") {
			if idx := strings.LastIndex(trimmed, ":"); idx > 0 {
				currentFile = trimmed[len("# configuration file "):idx]
			}
			continue
		}

		// Handle pending http block (split-line "http\n{")
		if pendingHTTP {
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			pendingHTTP = false
			if strings.Contains(trimmed, "{") {
				inHTTP = true
				seenHTTP = true
				httpDepth = 1
				continue
			}
			// Not a "{" line — fall through to process normally
		}

		// Handle pending server block (split-line "server\n{")
		if pendingServer {
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			pendingServer = false
			if strings.Contains(trimmed, "{") {
				current = &serverBlock{
					configFile: currentFile,
					depth:      1,
				}
				continue
			}
			// Not a "{" line — fall through to process normally
		}

		// Track http block start
		if !inHTTP && isNginxBlockKeyword(trimmed, "http") {
			if strings.Contains(trimmed, "{") {
				inHTTP = true
				seenHTTP = true
				httpDepth = 1
				continue
			}
			pendingHTTP = true
			continue
		}

		// Detect server block start (inside or after http context). Uses seenHTTP
		// to catch server blocks from included files listed separately in nginx -T
		// output (they appear after the http {} block closes). BEFORE httpDepth
		// counting so the "{" in "server {" is only tracked in current.depth,
		// not double-counted in httpDepth.
		if (inHTTP || seenHTTP) && current == nil && isNginxBlockKeyword(trimmed, "server") {
			if strings.Contains(trimmed, "{") {
				current = &serverBlock{
					configFile: currentFile,
					depth:      1,
				}
				continue
			}
			pendingServer = true
			continue
		}

		// Track brace depth within the http block (non-server lines only)
		if inHTTP && current == nil {
			for _, ch := range trimmed {
				if ch == '{' {
					httpDepth++
				} else if ch == '}' {
					httpDepth--
					if httpDepth == 0 {
						inHTTP = false
					}
				}
			}
		}

		// Collect lines inside server block and track depth
		if current != nil {
			for _, ch := range trimmed {
				if ch == '{' {
					current.depth++
				} else if ch == '}' {
					current.depth--
				}
			}

			if current.depth <= 0 {
				site := parseNginxServerBlock(current.lines, current.configFile)
				sites = append(sites, site)
				current = nil
			} else {
				current.lines = append(current.lines, trimmed)
			}
		}
	}

	return sites
}

// isNginxBlockKeyword checks if trimmed starts with keyword as a standalone
// block directive (followed by whitespace, '{', or end of line).
// This prevents "server_name" from matching "server", etc.
func isNginxBlockKeyword(trimmed, keyword string) bool {
	if !strings.HasPrefix(trimmed, keyword) {
		return false
	}
	if len(trimmed) == len(keyword) {
		return true
	}
	next := trimmed[len(keyword)]
	return next == ' ' || next == '\t' || next == '{'
}

func parseNginxServerBlock(lines []string, configFile string) models.ProxySite {
	site := models.ProxySite{
		Enabled:    true,
		ConfigFile: configFile,
	}

	for _, line := range lines {
		line = strings.TrimSuffix(strings.TrimSpace(line), ";")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		switch fields[0] {
		case "server_name":
			for _, name := range fields[1:] {
				if name != "_" { // skip catch-all
					site.ServerNames = append(site.ServerNames, name)
				}
			}
		case "listen":
			site.Listen = append(site.Listen, strings.Join(fields[1:], " "))
		case "root":
			site.Root = fields[1]
		case "proxy_pass":
			site.ProxyPass = fields[1]
		case "ssl_certificate":
			site.SSLCert = fields[1]
		}
	}

	// Check enabled status from sites-enabled/sites-available pattern
	if configFile != "" && strings.Contains(configFile, "sites-available") {
		site.Enabled = false
	}

	return site
}

// parseNginxUpstreams extracts upstream blocks from nginx config.
func parseNginxUpstreams(config string) []models.ProxyUpstream {
	var upstreams []models.ProxyUpstream
	re := regexp.MustCompile(`(?s)upstream\s+(\S+)\s*\{([^}]*)\}`)

	for _, match := range re.FindAllStringSubmatch(config, -1) {
		name := match[1]
		body := match[2]
		upstream := models.ProxyUpstream{Name: name}

		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSuffix(strings.TrimSpace(line), ";")
			fields := strings.Fields(line)
			if len(fields) == 0 || fields[0] != "server" {
				continue
			}
			if len(fields) < 2 {
				continue
			}

			srv := models.UpstreamServer{Address: fields[1]}
			for _, param := range fields[2:] {
				switch {
				case strings.HasPrefix(param, "weight="):
					if w, err := strconv.Atoi(strings.TrimPrefix(param, "weight=")); err == nil {
						srv.Weight = w
					}
				case param == "backup":
					srv.Backup = true
				case param == "down":
					srv.Down = true
				}
			}
			upstream.Servers = append(upstream.Servers, srv)
		}

		upstreams = append(upstreams, upstream)
	}

	return upstreams
}

// parseNginxSecurity extracts security-related directives from nginx config.
func parseNginxSecurity(config string) *models.ProxySecurityCfg {
	sec := &models.ProxySecurityCfg{
		SecurityHeaders: make(map[string]string),
	}

	for _, line := range strings.Split(config, "\n") {
		trimmed := strings.TrimSuffix(strings.TrimSpace(line), ";")
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}

		switch fields[0] {
		case "limit_req_zone":
			rule := models.RateLimitRule{}
			// limit_req_zone $binary_remote_addr zone=name:size rate=10r/s
			for _, f := range fields[1:] {
				if strings.HasPrefix(f, "zone=") {
					parts := strings.SplitN(strings.TrimPrefix(f, "zone="), ":", 2)
					rule.Zone = parts[0]
				}
				if strings.HasPrefix(f, "rate=") {
					rule.Rate = strings.TrimPrefix(f, "rate=")
				}
			}
			if rule.Zone != "" || rule.Rate != "" {
				sec.RateLimiting = append(sec.RateLimiting, rule)
			}

		case "limit_req":
			// limit_req zone=name burst=5
			for i, r := range sec.RateLimiting {
				for _, f := range fields[1:] {
					if strings.HasPrefix(f, "zone=") {
						zoneName := strings.TrimPrefix(f, "zone=")
						if r.Zone == zoneName {
							for _, ff := range fields[1:] {
								if strings.HasPrefix(ff, "burst=") {
									if b, err := strconv.Atoi(strings.TrimPrefix(ff, "burst=")); err == nil {
										sec.RateLimiting[i].Burst = b
									}
								}
							}
						}
					}
				}
			}

		case "allow", "deny":
			sec.AccessControls = append(sec.AccessControls, models.AccessRule{
				Directive: fields[0],
				Value:     fields[1],
			})

		case "add_header":
			if len(fields) >= 3 {
				headerName := fields[1]
				// Rejoin value fields, dropping the trailing "always" nginx parameter
				valFields := fields[2:]
				if len(valFields) > 1 && valFields[len(valFields)-1] == "always" {
					valFields = valFields[:len(valFields)-1]
				}
				headerVal := strings.Trim(strings.Join(valFields, " "), "'\"")
				// Only track security-relevant headers
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
		}
	}

	// Only return if there's actual security config
	if len(sec.RateLimiting) == 0 && len(sec.AccessControls) == 0 && len(sec.SecurityHeaders) == 0 {
		return nil
	}

	return sec
}

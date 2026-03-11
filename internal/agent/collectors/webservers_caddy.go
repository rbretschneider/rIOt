package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// CaddyParser detects and parses Caddy server configuration.
type CaddyParser struct{}

func (p *CaddyParser) Name() string { return "caddy" }

func (p *CaddyParser) Detect(ctx context.Context) *models.ProxyServer {
	path, err := lookBinary("caddy", "/usr/bin/caddy", "/usr/local/bin/caddy", "/usr/sbin/caddy")
	if err != nil {
		return nil
	}

	srv := &models.ProxyServer{
		Name:   "caddy",
		Status: "unknown",
	}

	// Version
	if out, err := exec.CommandContext(ctx, path, "version").Output(); err == nil {
		v := strings.TrimSpace(string(out))
		// Output: "v2.7.6 h1:abc123..."
		if fields := strings.Fields(v); len(fields) > 0 {
			srv.Version = fields[0]
		}
	}

	// Status via systemctl
	if out, err := exec.CommandContext(ctx, "systemctl", "is-active", "caddy").Output(); err == nil {
		state := strings.TrimSpace(string(out))
		if state == "active" {
			srv.Status = "running"
		} else {
			srv.Status = state
		}
	}

	// PID
	if out, err := exec.CommandContext(ctx, "systemctl", "show", "caddy", "--property=MainPID", "--value").Output(); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && pid > 0 {
			srv.PID = pid
		}
	}

	// Config path — check common locations
	for _, p := range []string{"/etc/caddy/Caddyfile", "/etc/caddy/caddy.json"} {
		if _, err := os.Stat(p); err == nil {
			srv.ConfigPath = p
			break
		}
	}

	return srv
}

func (p *CaddyParser) Parse(ctx context.Context, server *models.ProxyServer) error {
	// Try admin API first (structured JSON)
	if p.parseFromAPI(ctx, server) {
		// Scan cert directory even if API works
		p.scanCerts(server)
		return nil
	}

	// Fall back to Caddyfile parsing
	if server.ConfigPath != "" {
		p.parseCaddyfile(server)
	}

	// Scan cert directory
	p.scanCerts(server)

	return nil
}

// caddyAPIConfig represents relevant parts of Caddy's admin API response.
type caddyAPIConfig struct {
	Apps struct {
		HTTP struct {
			Servers map[string]caddyHTTPServer `json:"servers"`
		} `json:"http"`
	} `json:"apps"`
}

type caddyHTTPServer struct {
	Listen []string         `json:"listen"`
	Routes []caddyRoute     `json:"routes"`
}

type caddyRoute struct {
	Match   []caddyMatch   `json:"match"`
	Handle  []caddyHandler `json:"handle"`
}

type caddyMatch struct {
	Host []string `json:"host"`
}

type caddyHandler struct {
	Handler   string           `json:"handler"`
	Upstreams []caddyUpstream  `json:"upstreams,omitempty"`
	Headers   *caddyHeaders    `json:"headers,omitempty"`
	Routes    []caddyRoute     `json:"routes,omitempty"`
	Root      string           `json:"root,omitempty"`
}

type caddyUpstream struct {
	Dial string `json:"dial"`
}

type caddyHeaders struct {
	Response struct {
		Set map[string][]string `json:"set"`
	} `json:"response"`
}

func (p *CaddyParser) parseFromAPI(ctx context.Context, server *models.ProxyServer) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost:2019/config/", nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return false
	}

	var cfg caddyAPIConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return false
	}

	// Extract sites and upstreams from API config
	upstreamMap := make(map[string]*models.ProxyUpstream)
	secHeaders := make(map[string]string)

	for _, httpServer := range cfg.Apps.HTTP.Servers {
		for _, route := range httpServer.Routes {
			site := models.ProxySite{Enabled: true}

			// Extract hostnames from match
			for _, m := range route.Match {
				site.ServerNames = append(site.ServerNames, m.Host...)
			}

			// Extract listen ports
			site.Listen = httpServer.Listen

			// Extract handlers
			for _, h := range route.Handle {
				switch h.Handler {
				case "reverse_proxy":
					for i, u := range h.Upstreams {
						site.ProxyPass = u.Dial
						// Build upstream group
						upName := fmt.Sprintf("upstream_%d", len(upstreamMap))
						if len(site.ServerNames) > 0 {
							upName = site.ServerNames[0]
						}
						if _, ok := upstreamMap[upName]; !ok {
							upstreamMap[upName] = &models.ProxyUpstream{Name: upName}
						}
						upstreamMap[upName].Servers = append(upstreamMap[upName].Servers, models.UpstreamServer{
							Address: u.Dial,
							Weight:  i + 1,
						})
					}
				case "file_server":
					site.Root = h.Root
				case "headers":
					if h.Headers != nil {
						for k, v := range h.Headers.Response.Set {
							if len(v) > 0 {
								secHeaders[k] = v[0]
							}
						}
					}
				}
			}

			if len(site.ServerNames) > 0 || site.ProxyPass != "" || site.Root != "" {
				server.Sites = append(server.Sites, site)
			}
		}
	}

	// Convert upstream map to slice
	for _, u := range upstreamMap {
		server.Upstreams = append(server.Upstreams, *u)
	}

	// Security headers
	if len(secHeaders) > 0 {
		if server.SecurityConfig == nil {
			server.SecurityConfig = &models.ProxySecurityCfg{}
		}
		server.SecurityConfig.SecurityHeaders = secHeaders
	}

	return true
}

func (p *CaddyParser) parseCaddyfile(server *models.ProxyServer) {
	data, err := os.ReadFile(server.ConfigPath)
	if err != nil {
		return
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	var currentSite *models.ProxySite
	depth := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Count braces
		openBraces := strings.Count(trimmed, "{")
		closeBraces := strings.Count(trimmed, "}")

		if depth == 0 && openBraces > 0 {
			// New site block: "example.com {"
			addr := strings.TrimSuffix(strings.TrimSpace(strings.Split(trimmed, "{")[0]), " ")
			if addr != "" {
				currentSite = &models.ProxySite{
					Enabled: true,
				}
				// Parse address — could be "example.com", ":443", "http://example.com"
				for _, a := range strings.Fields(addr) {
					a = strings.TrimPrefix(a, "http://")
					a = strings.TrimPrefix(a, "https://")
					if strings.HasPrefix(a, ":") {
						currentSite.Listen = append(currentSite.Listen, a)
					} else {
						currentSite.ServerNames = append(currentSite.ServerNames, a)
					}
				}
			}
			depth += openBraces - closeBraces
			continue
		}

		depth += openBraces - closeBraces

		if currentSite != nil && depth >= 1 {
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 {
				switch fields[0] {
				case "reverse_proxy":
					currentSite.ProxyPass = fields[1]
				case "root":
					// root * /var/www/html
					if len(fields) >= 3 {
						currentSite.Root = fields[2]
					} else {
						currentSite.Root = fields[1]
					}
				}
			}
		}

		if depth <= 0 && currentSite != nil {
			server.Sites = append(server.Sites, *currentSite)
			currentSite = nil
			depth = 0
		}
	}
}

// scanCerts scans Caddy's certificate storage directory.
func (p *CaddyParser) scanCerts(server *models.ProxyServer) {
	// Caddy stores certs under XDG_DATA_HOME/caddy/certificates/ or /var/lib/caddy/.local/share/caddy/certificates/
	certDirs := []string{
		"/var/lib/caddy/.local/share/caddy/certificates",
	}
	if home := os.Getenv("XDG_DATA_HOME"); home != "" {
		certDirs = append([]string{filepath.Join(home, "caddy", "certificates")}, certDirs...)
	}
	if home := os.Getenv("HOME"); home != "" {
		certDirs = append([]string{filepath.Join(home, ".local", "share", "caddy", "certificates")}, certDirs...)
	}

	for _, dir := range certDirs {
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

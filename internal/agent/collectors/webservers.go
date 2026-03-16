package collectors

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// ProxyParser is the interface for detecting and parsing a specific proxy server.
type ProxyParser interface {
	Name() string
	Detect(ctx context.Context) *models.ProxyServer
	Parse(ctx context.Context, server *models.ProxyServer) error
}

// WebServersCollector discovers and inspects reverse proxy / web servers.
// Linux-only; returns empty on other platforms.
type WebServersCollector struct{}

func (c *WebServersCollector) Name() string { return "webservers" }

func (c *WebServersCollector) Collect(ctx context.Context) (interface{}, error) {
	if runtime.GOOS != "linux" {
		return &models.WebServerInfo{}, nil
	}

	parsers := []ProxyParser{
		&NginxParser{},
		&CaddyParser{},
		&FerronParser{},
	}

	var servers []models.ProxyServer
	for _, p := range parsers {
		srv := p.Detect(ctx)
		if srv == nil {
			slog.Debug("web server not found", "parser", p.Name())
			continue
		}
		slog.Debug("web server detected", "parser", p.Name(), "version", srv.Version, "status", srv.Status)
		if err := p.Parse(ctx, srv); err != nil {
			slog.Warn("web server parse failed", "parser", p.Name(), "error", err)
		}
		// Deduplicate certs by fingerprint
		srv.Certs = deduplicateCerts(srv.Certs)
		servers = append(servers, *srv)
	}

	return &models.WebServerInfo{Servers: servers}, nil
}

// lookBinary finds a binary by name using LookPath, falling back to common
// system paths that may not be in the systemd service PATH.
func lookBinary(name string, fallbackPaths ...string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	for _, p := range fallbackPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s not found", name)
}

// parseCertFile reads a PEM certificate file and extracts metadata.
func parseCertFile(path string) (*models.ProxyCert, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}

	fingerprint := fmt.Sprintf("%x", sha256.Sum256(cert.Raw))
	daysLeft := int(math.Floor(time.Until(cert.NotAfter).Hours() / 24))

	keyType := "Unknown"
	if cert.PublicKeyAlgorithm != x509.UnknownPublicKeyAlgorithm {
		keyType = cert.PublicKeyAlgorithm.String()
	}

	var sans []string
	sans = append(sans, cert.DNSNames...)
	for _, ip := range cert.IPAddresses {
		sans = append(sans, ip.String())
	}

	subject := cert.Subject.CommonName
	if subject == "" && len(sans) > 0 {
		subject = sans[0]
	}

	issuer := cert.Issuer.CommonName
	if issuer == "" {
		issuer = strings.Join(cert.Issuer.Organization, ", ")
	}

	return &models.ProxyCert{
		FilePath:    path,
		Subject:     subject,
		Issuer:      issuer,
		SANs:        sans,
		NotBefore:   cert.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:    cert.NotAfter.UTC().Format(time.RFC3339),
		DaysLeft:    daysLeft,
		KeyType:     keyType,
		IsCA:        cert.IsCA,
		Fingerprint: fingerprint,
	}, nil
}

// deduplicateCerts removes duplicate certs by SHA-256 fingerprint.
func deduplicateCerts(certs []models.ProxyCert) []models.ProxyCert {
	if len(certs) == 0 {
		return certs
	}
	seen := make(map[string]bool, len(certs))
	var out []models.ProxyCert
	for _, c := range certs {
		if seen[c.Fingerprint] {
			continue
		}
		seen[c.Fingerprint] = true
		out = append(out, c)
	}
	return out
}

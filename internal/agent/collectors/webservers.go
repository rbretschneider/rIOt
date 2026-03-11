package collectors

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math"
	"os"
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
	}

	var servers []models.ProxyServer
	for _, p := range parsers {
		srv := p.Detect(ctx)
		if srv == nil {
			continue
		}
		_ = p.Parse(ctx, srv)
		// Deduplicate certs by fingerprint
		srv.Certs = deduplicateCerts(srv.Certs)
		servers = append(servers, *srv)
	}

	return &models.WebServerInfo{Servers: servers}, nil
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

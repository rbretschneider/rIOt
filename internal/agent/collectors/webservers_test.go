package collectors

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func TestParseCertFile(t *testing.T) {
	// Generate a self-signed test certificate
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	notBefore := time.Now().Add(-24 * time.Hour)
	notAfter := time.Now().Add(90 * 24 * time.Hour)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test.example.com",
		},
		Issuer: pkix.Name{
			CommonName:   "Test CA",
			Organization: []string{"Test Org"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"test.example.com", "www.test.example.com"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "test.pem")
	f, err := os.Create(certPath)
	require.NoError(t, err)
	err = pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	require.NoError(t, err)
	f.Close()

	cert, err := parseCertFile(certPath)
	require.NoError(t, err)

	assert.Equal(t, certPath, cert.FilePath)
	assert.Equal(t, "test.example.com", cert.Subject)
	assert.Contains(t, cert.SANs, "test.example.com")
	assert.Contains(t, cert.SANs, "www.test.example.com")
	assert.Equal(t, "ECDSA", cert.KeyType)
	assert.False(t, cert.IsCA)
	assert.NotEmpty(t, cert.Fingerprint)
	assert.True(t, cert.DaysLeft > 85 && cert.DaysLeft <= 90)
	assert.NotEmpty(t, cert.NotBefore)
	assert.NotEmpty(t, cert.NotAfter)
}

func TestParseCertFile_Invalid(t *testing.T) {
	tmpDir := t.TempDir()

	// Test non-existent file
	_, err := parseCertFile(filepath.Join(tmpDir, "nonexistent.pem"))
	assert.Error(t, err)

	// Test invalid PEM
	badPath := filepath.Join(tmpDir, "bad.pem")
	os.WriteFile(badPath, []byte("not a pem file"), 0644)
	_, err = parseCertFile(badPath)
	assert.Error(t, err)
}

func TestDeduplicateCerts(t *testing.T) {
	certs := []models.ProxyCert{
		{FilePath: "/a.pem", Subject: "a.com", Fingerprint: "aaa"},
		{FilePath: "/b.pem", Subject: "b.com", Fingerprint: "bbb"},
		{FilePath: "/c.pem", Subject: "a.com", Fingerprint: "aaa"}, // duplicate
		{FilePath: "/d.pem", Subject: "c.com", Fingerprint: "ccc"},
	}

	result := deduplicateCerts(certs)
	assert.Len(t, result, 3)
	assert.Equal(t, "aaa", result[0].Fingerprint)
	assert.Equal(t, "bbb", result[1].Fingerprint)
	assert.Equal(t, "ccc", result[2].Fingerprint)
}

func TestDeduplicateCerts_Empty(t *testing.T) {
	assert.Nil(t, deduplicateCerts(nil))
	assert.Empty(t, deduplicateCerts([]models.ProxyCert{}))
}

func TestParseNginxSites(t *testing.T) {
	config := `# configuration file /etc/nginx/nginx.conf:
http {
    # configuration file /etc/nginx/sites-enabled/default.conf:
    server {
        listen 80;
        listen 443 ssl;
        server_name example.com www.example.com;
        ssl_certificate /etc/ssl/certs/example.pem;
        root /var/www/html;
    }

    # configuration file /etc/nginx/sites-enabled/app.conf:
    server {
        listen 443 ssl;
        server_name app.example.com;
        ssl_certificate /etc/ssl/certs/app.pem;
        proxy_pass http://127.0.0.1:8080;
    }
}`

	sites := parseNginxSites(config)
	require.Len(t, sites, 2)

	// First site
	assert.Equal(t, []string{"example.com", "www.example.com"}, sites[0].ServerNames)
	assert.Contains(t, sites[0].Listen, "80")
	assert.Contains(t, sites[0].Listen, "443 ssl")
	assert.Equal(t, "/etc/ssl/certs/example.pem", sites[0].SSLCert)
	assert.Equal(t, "/var/www/html", sites[0].Root)
	assert.Equal(t, "/etc/nginx/sites-enabled/default.conf", sites[0].ConfigFile)

	// Second site
	assert.Equal(t, []string{"app.example.com"}, sites[1].ServerNames)
	assert.Equal(t, "http://127.0.0.1:8080", sites[1].ProxyPass)
}

func TestParseNginxUpstreams(t *testing.T) {
	config := `
upstream backend {
    server 10.0.0.1:8080 weight=3;
    server 10.0.0.2:8080;
    server 10.0.0.3:8080 backup;
    server 10.0.0.4:8080 down;
}`

	upstreams := parseNginxUpstreams(config)
	require.Len(t, upstreams, 1)
	assert.Equal(t, "backend", upstreams[0].Name)
	require.Len(t, upstreams[0].Servers, 4)

	assert.Equal(t, "10.0.0.1:8080", upstreams[0].Servers[0].Address)
	assert.Equal(t, 3, upstreams[0].Servers[0].Weight)
	assert.False(t, upstreams[0].Servers[0].Backup)

	assert.True(t, upstreams[0].Servers[2].Backup)
	assert.True(t, upstreams[0].Servers[3].Down)
}

func TestParseNginxSecurity(t *testing.T) {
	config := `
limit_req_zone $binary_remote_addr zone=api:10m rate=10r/s;
limit_req zone=api burst=20;
allow 10.0.0.0/8;
deny all;
add_header Strict-Transport-Security "max-age=63072000" always;
add_header X-Frame-Options "DENY" always;
add_header X-Custom-Header "ignored";
`

	sec := parseNginxSecurity(config)
	require.NotNil(t, sec)

	// Rate limiting
	require.Len(t, sec.RateLimiting, 1)
	assert.Equal(t, "api", sec.RateLimiting[0].Zone)
	assert.Equal(t, "10r/s", sec.RateLimiting[0].Rate)
	assert.Equal(t, 20, sec.RateLimiting[0].Burst)

	// Access controls
	require.Len(t, sec.AccessControls, 2)
	assert.Equal(t, "allow", sec.AccessControls[0].Directive)
	assert.Equal(t, "10.0.0.0/8", sec.AccessControls[0].Value)
	assert.Equal(t, "deny", sec.AccessControls[1].Directive)
	assert.Equal(t, "all", sec.AccessControls[1].Value)

	// Security headers (only recognized ones, "always" nginx param stripped)
	assert.Equal(t, "max-age=63072000", sec.SecurityHeaders["Strict-Transport-Security"])
	assert.Equal(t, "DENY", sec.SecurityHeaders["X-Frame-Options"])
	_, hasCustom := sec.SecurityHeaders["X-Custom-Header"]
	assert.False(t, hasCustom)
}

func TestParseNginxSecurity_Empty(t *testing.T) {
	sec := parseNginxSecurity("server { listen 80; }")
	assert.Nil(t, sec)
}

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

func TestParseNginxSites_RealisticDebian(t *testing.T) {
	config := `# configuration file /etc/nginx/nginx.conf:
user www-data;
worker_processes auto;
pid /run/nginx.pid;

events {
    worker_connections 768;
}

http {
    sendfile on;
    tcp_nopush on;
    types_hash_max_size 2048;

    # configuration file /etc/nginx/mime.types:
    types {
        text/html html htm shtml;
        text/css css;
        application/javascript js;
    }

    access_log /var/log/nginx/access.log;
    error_log /var/log/nginx/error.log;

    gzip on;

    map $http_upgrade $connection_upgrade {
        default upgrade;
        '' close;
    }

    # configuration file /etc/nginx/sites-enabled/default:
    server {
        listen 80 default_server;
        listen [::]:80 default_server;
        server_name _;
        root /var/www/html;
    }

    # configuration file /etc/nginx/sites-enabled/app.conf:
    server {
        listen 443 ssl;
        server_name app.example.com;
        ssl_certificate /etc/letsencrypt/live/app.example.com/fullchain.pem;

        location / {
            proxy_pass http://127.0.0.1:3000;
        }

        location /api {
            proxy_pass http://127.0.0.1:8080;
        }
    }

    # configuration file /etc/nginx/sites-enabled/static.conf:
    server {
        listen 443 ssl;
        server_name static.example.com;
        ssl_certificate /etc/letsencrypt/live/static.example.com/fullchain.pem;
        root /var/www/static;
    }
}`

	sites := parseNginxSites(config)
	require.Len(t, sites, 3)

	// Default server (catch-all, server_name _ is excluded)
	assert.Empty(t, sites[0].ServerNames)
	assert.Contains(t, sites[0].Listen, "80 default_server")
	assert.Equal(t, "/var/www/html", sites[0].Root)

	// App server with nested locations
	assert.Equal(t, []string{"app.example.com"}, sites[1].ServerNames)
	assert.Equal(t, "/etc/letsencrypt/live/app.example.com/fullchain.pem", sites[1].SSLCert)

	// Static server
	assert.Equal(t, []string{"static.example.com"}, sites[2].ServerNames)
	assert.Equal(t, "/var/www/static", sites[2].Root)
}

func TestParseNginxSites_SplitLineHTTP(t *testing.T) {
	config := `# configuration file /etc/nginx/nginx.conf:
events {
    worker_connections 768;
}

http
{
    server {
        listen 80;
        server_name split.example.com;
    }
}`

	sites := parseNginxSites(config)
	require.Len(t, sites, 1)
	assert.Equal(t, []string{"split.example.com"}, sites[0].ServerNames)
}

func TestParseNginxSites_SplitLineServer(t *testing.T) {
	config := `http {
    server
    {
        listen 80;
        server_name split-server.example.com;
    }
}`

	sites := parseNginxSites(config)
	require.Len(t, sites, 1)
	assert.Equal(t, []string{"split-server.example.com"}, sites[0].ServerNames)
}

func TestParseNginxSites_NestedLocations(t *testing.T) {
	config := `http {
    server {
        listen 443 ssl;
        server_name nested.example.com;
        ssl_certificate /etc/ssl/nested.pem;

        location / {
            proxy_pass http://127.0.0.1:3000;
        }

        location /api {
            proxy_pass http://127.0.0.1:8080;

            location /api/v2 {
                proxy_pass http://127.0.0.1:9090;
            }
        }

        location ~ \.php$ {
            fastcgi_pass unix:/run/php/php-fpm.sock;
        }
    }
    server {
        listen 80;
        server_name other.example.com;
    }
}`

	sites := parseNginxSites(config)
	require.Len(t, sites, 2)

	assert.Equal(t, []string{"nested.example.com"}, sites[0].ServerNames)
	assert.Equal(t, "/etc/ssl/nested.pem", sites[0].SSLCert)

	assert.Equal(t, []string{"other.example.com"}, sites[1].ServerNames)
}

func TestParseNginxSites_CommentsWithBraces(t *testing.T) {
	config := `http {
    # This is a comment about server {
    # } end of comment
    #server {
    #    listen 9999;
    #}
    server {
        listen 80;
        server_name real.example.com;
    }
}`

	sites := parseNginxSites(config)
	require.Len(t, sites, 1)
	assert.Equal(t, []string{"real.example.com"}, sites[0].ServerNames)
}

func TestNginxReadConfigFromDisk(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a sites-enabled directory with a site config
	sitesDir := filepath.Join(tmpDir, "sites-enabled")
	os.MkdirAll(sitesDir, 0755)

	siteConf := `server {
    listen 80;
    listen 443 ssl;
    server_name example.com;
    ssl_certificate /etc/ssl/certs/example.pem;
    proxy_pass http://127.0.0.1:3000;
}
`
	os.WriteFile(filepath.Join(sitesDir, "example.conf"), []byte(siteConf), 0644)

	// Create main nginx.conf that includes the sites directory
	mainConf := `events { worker_connections 768; }
http {
    include ` + filepath.ToSlash(filepath.Join(sitesDir, "*")) + `;
}
`
	mainPath := filepath.Join(tmpDir, "nginx.conf")
	os.WriteFile(mainPath, []byte(mainConf), 0644)

	parser := &NginxParser{}
	config := parser.readConfigFromDisk(mainPath)

	// Should contain both files' markers
	assert.Contains(t, config, "# configuration file ")
	assert.Contains(t, config, "nginx.conf")
	assert.Contains(t, config, "example.conf")

	// Parse sites from the disk-read config
	sites := parseNginxSites(config)
	require.Len(t, sites, 1)
	assert.Equal(t, []string{"example.com"}, sites[0].ServerNames)
	assert.Equal(t, "/etc/ssl/certs/example.pem", sites[0].SSLCert)
	assert.Equal(t, "http://127.0.0.1:3000", sites[0].ProxyPass)
}

// TestParseNginxSites_NginxTSeparateFiles tests the real nginx -T output format
// where included files are listed as separate sections AFTER the http block closes,
// rather than inlined within the http {} braces.
func TestParseNginxSites_NginxTSeparateFiles(t *testing.T) {
	config := `# configuration file /etc/nginx/nginx.conf:
user www-data;
worker_processes auto;

events {
    worker_connections 768;
}

http {
    sendfile on;
    tcp_nopush on;

    include /etc/nginx/mime.types;
    include /etc/nginx/conf.d/*.conf;
    include /etc/nginx/sites-enabled/*;
}

# configuration file /etc/nginx/mime.types:
types {
    text/html html;
    text/css css;
}

# configuration file /etc/nginx/sites-enabled/site1:
server {
    listen 80;
    server_name site1.example.com;
    proxy_pass http://127.0.0.1:3000;
}

# configuration file /etc/nginx/sites-enabled/site2:
server {
    listen 443 ssl;
    server_name site2.example.com;
    ssl_certificate /etc/ssl/certs/site2.pem;
    root /var/www/site2;
}
`
	sites := parseNginxSites(config)
	require.Len(t, sites, 2, "should find server blocks listed after http {} closes")

	assert.Equal(t, []string{"site1.example.com"}, sites[0].ServerNames)
	assert.Equal(t, "http://127.0.0.1:3000", sites[0].ProxyPass)
	assert.Equal(t, "/etc/nginx/sites-enabled/site1", sites[0].ConfigFile)

	assert.Equal(t, []string{"site2.example.com"}, sites[1].ServerNames)
	assert.Equal(t, "/var/www/site2", sites[1].Root)
	assert.Equal(t, "/etc/ssl/certs/site2.pem", sites[1].SSLCert)
	assert.Equal(t, "/etc/nginx/sites-enabled/site2", sites[1].ConfigFile)
}

func TestNginxReadConfigFromDisk_NonexistentPath(t *testing.T) {
	parser := &NginxParser{}
	config := parser.readConfigFromDisk("/tmp/nonexistent-riot-test/nginx.conf")
	assert.Empty(t, config)
}

// --- Ferron parser tests ---

func TestParseFerronConfig_BasicSites(t *testing.T) {
	config := `
example.com {
	root "/var/www/html"
}

"api.example.com" {
	proxy "http://localhost:3000"
}
`
	sites, _, _ := parseFerronConfig(config)
	require.Len(t, sites, 2)

	assert.Equal(t, []string{"example.com"}, sites[0].ServerNames)
	assert.Equal(t, "/var/www/html", sites[0].Root)
	assert.True(t, sites[0].Enabled)

	assert.Equal(t, []string{"api.example.com"}, sites[1].ServerNames)
	assert.Equal(t, "http://localhost:3000", sites[1].ProxyPass)
}

func TestParseFerronConfig_MultiHost(t *testing.T) {
	config := `
"example.com,example.org" {
	root "/var/www/html"
}
`
	sites, _, _ := parseFerronConfig(config)
	require.Len(t, sites, 1)
	assert.Equal(t, []string{"example.com", "example.org"}, sites[0].ServerNames)
}

func TestParseFerronConfig_HostWithPort(t *testing.T) {
	config := `
"example.com:8080" {
	root "/var/www/html"
}
`
	sites, _, _ := parseFerronConfig(config)
	require.Len(t, sites, 1)
	assert.Equal(t, []string{"example.com"}, sites[0].ServerNames)
	assert.Contains(t, sites[0].Listen, "8080")
}

func TestParseFerronConfig_TLS(t *testing.T) {
	config := `
example.com {
	tls "/etc/ssl/certs/example.pem" "/etc/ssl/private/example.key"
}
`
	sites, _, _ := parseFerronConfig(config)
	require.Len(t, sites, 1)
	assert.Equal(t, "/etc/ssl/certs/example.pem", sites[0].SSLCert)
}

func TestParseFerronConfig_Upstreams(t *testing.T) {
	config := `
api.example.com {
	proxy "http://backend1:8080"
	proxy "http://backend2:8080"
}
`
	sites, upstreams, _ := parseFerronConfig(config)
	require.Len(t, sites, 1)
	assert.Equal(t, "http://backend2:8080", sites[0].ProxyPass) // last proxy wins for display

	require.Len(t, upstreams, 1)
	assert.Equal(t, "api.example.com", upstreams[0].Name)
	require.Len(t, upstreams[0].Servers, 2)
	assert.Equal(t, "http://backend1:8080", upstreams[0].Servers[0].Address)
	assert.Equal(t, "http://backend2:8080", upstreams[0].Servers[1].Address)
}

func TestParseFerronConfig_SecurityHeaders(t *testing.T) {
	config := `
example.com {
	root "/var/www/html"
	header "Strict-Transport-Security" "max-age=31536000"
	header "X-Frame-Options" "DENY"
	header "X-Custom-Header" "ignored"
}
`
	sites, _, sec := parseFerronConfig(config)
	require.Len(t, sites, 1)
	require.NotNil(t, sec)
	assert.Equal(t, "max-age=31536000", sec.SecurityHeaders["Strict-Transport-Security"])
	assert.Equal(t, "DENY", sec.SecurityHeaders["X-Frame-Options"])
	_, hasCustom := sec.SecurityHeaders["X-Custom-Header"]
	assert.False(t, hasCustom)
}

func TestParseFerronConfig_RateLimiting(t *testing.T) {
	config := `
example.com {
	limit rate=100 burst=200
}
`
	_, _, sec := parseFerronConfig(config)
	require.NotNil(t, sec)
	require.Len(t, sec.RateLimiting, 1)
	assert.Equal(t, "100r/s", sec.RateLimiting[0].Rate)
	assert.Equal(t, 200, sec.RateLimiting[0].Burst)
}

func TestParseFerronConfig_AccessControl(t *testing.T) {
	config := `
example.com {
	block "192.168.1.0/24"
	allow "10.0.0.0/8"
}
`
	_, _, sec := parseFerronConfig(config)
	require.NotNil(t, sec)
	require.Len(t, sec.AccessControls, 2)
	assert.Equal(t, "deny", sec.AccessControls[0].Directive)
	assert.Equal(t, "192.168.1.0/24", sec.AccessControls[0].Value)
	assert.Equal(t, "allow", sec.AccessControls[1].Directive)
	assert.Equal(t, "10.0.0.0/8", sec.AccessControls[1].Value)
}

func TestParseFerronConfig_SkipsGlobalsAndSnippets(t *testing.T) {
	config := `
globals {
	tls_min_version "TLSv1.2"
}

snippet "common" {
	header "X-Frame-Options" "DENY"
}

example.com {
	root "/var/www/html"
}
`
	sites, _, _ := parseFerronConfig(config)
	require.Len(t, sites, 1)
	assert.Equal(t, []string{"example.com"}, sites[0].ServerNames)
}

func TestParseFerronConfig_Comments(t *testing.T) {
	config := `
// This is a comment
example.com {
	// root "/old/path"
	root "/var/www/html"
}
`
	sites, _, _ := parseFerronConfig(config)
	require.Len(t, sites, 1)
	assert.Equal(t, "/var/www/html", sites[0].Root)
}

func TestParseFerronConfig_NoSecurityConfig(t *testing.T) {
	config := `
example.com {
	root "/var/www/html"
}
`
	_, _, sec := parseFerronConfig(config)
	assert.Nil(t, sec)
}

func TestParseFerronConfig_Empty(t *testing.T) {
	sites, upstreams, sec := parseFerronConfig("")
	assert.Empty(t, sites)
	assert.Empty(t, upstreams)
	assert.Nil(t, sec)
}

func TestFerronLoadConfig_WithInclude(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an included config file
	incConf := `api.example.com {
	proxy "http://localhost:8080"
}
`
	incPath := filepath.Join(tmpDir, "api.kdl")
	os.WriteFile(incPath, []byte(incConf), 0644)

	// Create main config that includes it
	mainConf := `include "` + filepath.ToSlash(filepath.Join(tmpDir, "*.kdl")) + `"

example.com {
	root "/var/www/html"
}
`
	mainPath := filepath.Join(tmpDir, "ferron.kdl")
	os.WriteFile(mainPath, []byte(mainConf), 0644)

	parser := &FerronParser{}
	content, err := parser.loadConfig(mainPath)
	require.NoError(t, err)

	sites, _, _ := parseFerronConfig(content)
	require.Len(t, sites, 2)
}

func TestFerronLoadConfig_NonexistentPath(t *testing.T) {
	parser := &FerronParser{}
	_, err := parser.loadConfig("/tmp/nonexistent-riot-test/ferron.kdl")
	assert.Error(t, err)
}

func TestParseFerronConfig_Realistic(t *testing.T) {
	config := `globals {
	tls_min_version "TLSv1.2"
	auto_tls_contact "admin@example.com"
}

"blog.example.com" {
	root "/var/www/blog"
	header "Strict-Transport-Security" "max-age=31536000"
	header "X-Content-Type-Options" "nosniff"
	header "Referrer-Policy" "strict-origin-when-cross-origin"
	limit rate=50 burst=100
}

"api.example.com" {
	proxy "http://127.0.0.1:3000"
	proxy "http://127.0.0.1:3001"
	tls "/etc/ssl/certs/api.pem" "/etc/ssl/private/api.key"
	limit rate=10 burst=20
	block "192.168.0.0/16"
	allow "10.0.0.0/8"
}

"static.example.com" {
	root "/var/www/static"
}
`
	sites, upstreams, sec := parseFerronConfig(config)
	require.Len(t, sites, 3)

	// Blog site
	assert.Equal(t, []string{"blog.example.com"}, sites[0].ServerNames)
	assert.Equal(t, "/var/www/blog", sites[0].Root)

	// API site with multiple backends
	assert.Equal(t, []string{"api.example.com"}, sites[1].ServerNames)
	assert.Equal(t, "/etc/ssl/certs/api.pem", sites[1].SSLCert)
	require.Len(t, upstreams, 1)
	assert.Len(t, upstreams[0].Servers, 2)

	// Static site
	assert.Equal(t, []string{"static.example.com"}, sites[2].ServerNames)

	// Security config aggregated from all sites
	require.NotNil(t, sec)
	assert.Equal(t, "max-age=31536000", sec.SecurityHeaders["Strict-Transport-Security"])
	assert.Equal(t, "nosniff", sec.SecurityHeaders["X-Content-Type-Options"])
	assert.Len(t, sec.RateLimiting, 2)
	assert.Len(t, sec.AccessControls, 2)
}


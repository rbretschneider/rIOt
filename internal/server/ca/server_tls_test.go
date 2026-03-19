package ca

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"
)

func TestValidateSANs(t *testing.T) {
	tests := []struct {
		name    string
		sans    []string
		wantErr bool
	}{
		{"empty list", nil, false},
		{"valid hostname", []string{"my.ddns.org"}, false},
		{"valid IP", []string{"203.0.113.45"}, false},
		{"valid IPv6", []string{"::1"}, false},
		{"mixed valid", []string{"my.ddns.org", "10.0.0.1", "home.example.com"}, false},
		{"single label hostname", []string{"localhost"}, false},
		{"wildcard rejected", []string{"*.example.com"}, true},
		{"empty entry", []string{""}, true},
		{"too many", make([]string, 21), true},
		{"hostname too long", []string{string(make([]byte, 254))}, true},
		{"invalid chars", []string{"my host.com"}, true},
		{"label starts with hyphen", []string{"-bad.example.com"}, true},
		{"label ends with hyphen", []string{"bad-.example.com"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSANs(tt.sans)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSANs(%v) error = %v, wantErr %v", tt.sans, err, tt.wantErr)
			}
		})
	}
}

func TestParseSANs(t *testing.T) {
	dns, ips := ParseSANs([]string{"my.ddns.org", "10.0.0.1", "my.ddns.org", "  example.com  "})
	if len(dns) != 2 {
		t.Errorf("expected 2 DNS names, got %d: %v", len(dns), dns)
	}
	if len(ips) != 1 {
		t.Errorf("expected 1 IP, got %d: %v", len(ips), ips)
	}
}

func TestGenerateServerTLS_ExtraSANs(t *testing.T) {
	certPEM, _, err := GenerateServerTLS("192.168.1.100", nil, 1, "mylab.ddns.org", "10.99.0.1")
	if err != nil {
		t.Fatalf("GenerateServerTLS: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	// Check that DDNS hostname is in DNS SANs
	found := false
	for _, name := range cert.DNSNames {
		if name == "mylab.ddns.org" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("DDNS hostname not in cert DNSNames: %v", cert.DNSNames)
	}

	// Check that extra IP is in IP SANs
	target := net.ParseIP("10.99.0.1")
	found = false
	for _, ip := range cert.IPAddresses {
		if ip.Equal(target) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("extra IP 10.99.0.1 not in cert IPAddresses: %v", cert.IPAddresses)
	}

	// Verify cert is valid for the DDNS hostname
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	_, err = cert.Verify(x509.VerifyOptions{
		DNSName: "mylab.ddns.org",
		Roots:   pool,
	})
	if err != nil {
		t.Errorf("cert does not verify for mylab.ddns.org: %v", err)
	}
}

func TestGenerateServerTLS_NoExtraSANs(t *testing.T) {
	// Backwards compatibility: no extra SANs still works
	certPEM, _, err := GenerateServerTLS("192.168.1.100", nil, 1)
	if err != nil {
		t.Fatalf("GenerateServerTLS: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	cert, _ := x509.ParseCertificate(block.Bytes)

	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != "localhost" {
		t.Errorf("expected only localhost in DNSNames, got %v", cert.DNSNames)
	}
}

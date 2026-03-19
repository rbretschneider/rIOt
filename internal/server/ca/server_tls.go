package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"regexp"
	"strings"
	"time"
)

var hostnameLabel = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`)

// ValidateSANs validates a list of Subject Alternative Names.
// Each entry must be a valid IP address or DNS hostname.
// Returns an error describing the first invalid entry, or nil.
func ValidateSANs(sans []string) error {
	if len(sans) > 20 {
		return fmt.Errorf("too many SANs (max 20, got %d)", len(sans))
	}
	for _, s := range sans {
		s = strings.TrimSpace(s)
		if s == "" {
			return fmt.Errorf("empty SAN entry")
		}
		if strings.Contains(s, "*") {
			return fmt.Errorf("wildcards not allowed: %q", s)
		}
		// Valid if it's an IP address
		if net.ParseIP(s) != nil {
			continue
		}
		// Otherwise must be a valid hostname
		if len(s) > 253 {
			return fmt.Errorf("hostname too long: %q", s)
		}
		labels := strings.Split(s, ".")
		if len(labels) < 1 {
			return fmt.Errorf("invalid hostname: %q", s)
		}
		for _, label := range labels {
			if !hostnameLabel.MatchString(label) {
				return fmt.Errorf("invalid hostname label %q in %q", label, s)
			}
		}
	}
	return nil
}

// ParseSANs separates validated SAN entries into DNS names and IP addresses.
func ParseSANs(sans []string) (dnsNames []string, ips []net.IP) {
	seen := make(map[string]bool)
	for _, s := range sans {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		lower := strings.ToLower(s)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		if ip := net.ParseIP(s); ip != nil {
			ips = append(ips, ip)
		} else {
			dnsNames = append(dnsNames, s)
		}
	}
	return
}

// GenerateServerTLS creates a self-signed ECDSA P-256 server certificate.
// The cert includes SANs for the given hostname, "localhost", all detected
// local IP addresses, and any extra DNS names/IPs (e.g. DDNS hostnames).
func GenerateServerTLS(hostname string, extraIPs []net.IP, validityDays int, extraSANs ...string) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	// Collect SANs
	dnsNames := []string{"localhost"}
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}

	// Add hostname as DNS name or IP SAN depending on type
	if hostname != "" && hostname != "localhost" {
		if ip := net.ParseIP(hostname); ip != nil {
			ips = append(ips, ip)
		} else {
			dnsNames = append(dnsNames, hostname)
		}
	}

	ips = append(ips, extraIPs...)

	// Merge extra SANs (e.g. DDNS hostnames, external IPs)
	extraDNS, extraSANIPs := ParseSANs(extraSANs)
	dnsNames = append(dnsNames, extraDNS...)
	ips = append(ips, extraSANIPs...)

	// Add all local interface IPs
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				ips = append(ips, ipNet.IP)
			}
		}
	}

	// Deduplicate
	dnsNames = deduplicateDNSNames(dnsNames)
	ips = deduplicateIPs(ips)

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"rIOt"},
			CommonName:   hostname,
		},
		NotBefore:             now,
		NotAfter:              now.Add(time.Duration(validityDays) * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create cert: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// GenerateServerTLSWithCA creates a server certificate signed by the given CA.
// Agents that trust the CA will automatically accept the new certificate.
func GenerateServerTLSWithCA(authority *CA, hostname string, extraIPs []net.IP, validityDays int, extraSANs ...string) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	dnsNames := []string{"localhost"}
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}

	if hostname != "" && hostname != "localhost" {
		if ip := net.ParseIP(hostname); ip != nil {
			ips = append(ips, ip)
		} else {
			dnsNames = append(dnsNames, hostname)
		}
	}

	ips = append(ips, extraIPs...)

	// Merge extra SANs (e.g. DDNS hostnames, external IPs)
	extraDNS, extraSANIPs := ParseSANs(extraSANs)
	dnsNames = append(dnsNames, extraDNS...)
	ips = append(ips, extraSANIPs...)

	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				ips = append(ips, ipNet.IP)
			}
		}
	}

	// Deduplicate
	dnsNames = deduplicateDNSNames(dnsNames)
	ips = deduplicateIPs(ips)

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"rIOt"},
			CommonName:   hostname,
		},
		NotBefore:             now,
		NotAfter:              now.Add(time.Duration(validityDays) * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
	}

	// Sign with CA instead of self-signing
	certDER, err := x509.CreateCertificate(rand.Reader, template, authority.Cert(), &key.PublicKey, authority.key)
	if err != nil {
		return nil, nil, fmt.Errorf("create cert: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

func deduplicateDNSNames(names []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, n := range names {
		lower := strings.ToLower(n)
		if !seen[lower] {
			seen[lower] = true
			result = append(result, n)
		}
	}
	return result
}

func deduplicateIPs(ips []net.IP) []net.IP {
	seen := make(map[string]bool)
	var result []net.IP
	for _, ip := range ips {
		s := ip.String()
		if !seen[s] {
			seen[s] = true
			result = append(result, ip)
		}
	}
	return result
}

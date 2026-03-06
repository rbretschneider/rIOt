package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
	"time"
)

func TestNewCA(t *testing.T) {
	ca, err := NewCA()
	if err != nil {
		t.Fatal(err)
	}
	if ca.Cert() == nil {
		t.Fatal("cert is nil")
	}
	if !ca.Cert().IsCA {
		t.Error("cert is not CA")
	}
	if len(ca.CertPEM()) == 0 {
		t.Error("cert PEM is empty")
	}
	if len(ca.KeyPEM()) == 0 {
		t.Error("key PEM is empty")
	}
}

func TestLoadCA(t *testing.T) {
	ca, err := NewCA()
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadCA(ca.CertPEM(), ca.KeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Cert().Subject.CommonName != "rIOt CA" {
		t.Errorf("unexpected CN: %s", loaded.Cert().Subject.CommonName)
	}
}

func TestSignCSR(t *testing.T) {
	ca, err := NewCA()
	if err != nil {
		t.Fatal(err)
	}

	// Generate a client key and CSR
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "test-device"},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	certPEM, serial, notBefore, notAfter, err := ca.SignCSR(csrPEM, "device-123", 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(certPEM) == 0 {
		t.Error("cert PEM is empty")
	}
	if serial == "" {
		t.Error("serial is empty")
	}
	if notBefore.IsZero() || notAfter.IsZero() {
		t.Error("timestamps are zero")
	}

	// Verify the cert
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "device-123" {
		t.Errorf("unexpected CN: %s", cert.Subject.CommonName)
	}
	if len(cert.ExtKeyUsage) == 0 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Error("missing client auth EKU")
	}

	// Verify against CA
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert())
	_, err = cert.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		t.Errorf("cert verification failed: %v", err)
	}
}

func TestPEMRoundTrip(t *testing.T) {
	ca1, err := NewCA()
	if err != nil {
		t.Fatal(err)
	}

	ca2, err := LoadCA(ca1.CertPEM(), ca1.KeyPEM())
	if err != nil {
		t.Fatal(err)
	}

	// Both should be able to sign
	clientKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	csrTemplate := &x509.CertificateRequest{Subject: pkix.Name{CommonName: "test"}}
	csrDER, _ := x509.CreateCertificateRequest(rand.Reader, csrTemplate, clientKey)
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	_, _, _, _, err = ca2.SignCSR(csrPEM, "dev1", time.Hour)
	if err != nil {
		t.Errorf("round-tripped CA failed to sign: %v", err)
	}
}

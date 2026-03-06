package models

import "time"

// DeviceCert represents a client certificate issued to a device.
type DeviceCert struct {
	ID           int64      `json:"id"`
	DeviceID     string     `json:"device_id"`
	SerialNumber string     `json:"serial_number"`
	CertPEM      string     `json:"cert_pem,omitempty"`
	NotBefore    time.Time  `json:"not_before"`
	NotAfter     time.Time  `json:"not_after"`
	Revoked      bool       `json:"revoked"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// BootstrapKey is a single-use key for device enrollment.
type BootstrapKey struct {
	KeyHash      string     `json:"key_hash"`
	Label        string     `json:"label"`
	Used         bool       `json:"used"`
	UsedByDevice string     `json:"used_by_device,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
}

// EnrollRequest is sent by the agent to enroll with mTLS.
type EnrollRequest struct {
	CSRPEM       string `json:"csr_pem"`
	BootstrapKey string `json:"bootstrap_key"`
	Hostname     string `json:"hostname"`
	Arch         string `json:"arch,omitempty"`
}

// EnrollResponse is returned after successful enrollment.
type EnrollResponse struct {
	DeviceID string `json:"device_id"`
	CertPEM  string `json:"cert_pem"`
	CACertPEM string `json:"ca_cert_pem"`
}

package resilient

import (
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"
)

// ResilientHTTPClient wraps an http.Client with DNS fallback capabilities.
// On connection failure, it retries using cached IPs from the resolver.
type ResilientHTTPClient struct {
	inner    *http.Client
	resolver *ResilientResolver
	caPEM    []byte // for cert verification on IP fallback
}

// NewResilientHTTPClient creates a resilient HTTP client.
// If caPEM is provided, IP-based fallback connections will verify the server
// cert against this CA instead of relying on hostname verification.
func NewResilientHTTPClient(resolver *ResilientResolver, caPEM []byte) *ResilientHTTPClient {
	return &ResilientHTTPClient{
		inner: &http.Client{
			Timeout: 15 * time.Second,
		},
		resolver: resolver,
		caPEM:    caPEM,
	}
}

// Do executes a request with DNS fallback.
// Attempt 1: Normal request via hostname.
// Attempt 2: If DNS/connect fails, try cached IPs with Host header set.
func (c *ResilientHTTPClient) Do(req *http.Request) (*http.Response, error) {
	// Attempt 1: normal request
	resp, err := c.inner.Do(req)
	if err == nil {
		if c.resolver != nil {
			c.resolver.UpdateConnectTime(req.URL.Hostname())
		}
		return resp, nil
	}

	// Only fall back if we have a resolver and cached IPs
	if c.resolver == nil {
		return nil, err
	}

	hostname := req.URL.Hostname()
	cachedIPs := c.resolver.CachedIPs(hostname)
	if len(cachedIPs) == 0 {
		return nil, err
	}

	slog.Info("resilient HTTP: falling back to cached IPs", "hostname", hostname, "ips", cachedIPs)

	// Attempt 2: try each cached IP
	for _, ip := range cachedIPs {
		resp, fallbackErr := c.doWithIP(req, hostname, ip)
		if fallbackErr == nil {
			c.resolver.UpdateConnectTime(hostname)
			return resp, nil
		}
		slog.Debug("resilient HTTP: IP fallback failed", "ip", ip, "error", fallbackErr)
	}

	return nil, err // return original error
}

func (c *ResilientHTTPClient) doWithIP(origReq *http.Request, hostname, ip string) (*http.Response, error) {
	// Clone the request
	u := *origReq.URL
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	u.Host = net.JoinHostPort(ip, port)

	req := origReq.Clone(origReq.Context())
	req.URL = &u
	req.Host = hostname // preserve original Host header

	// Use a client with custom TLS that verifies against CA but with ServerName set
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: c.ipFallbackTLSConfig(hostname),
		},
	}

	return client.Do(req)
}

func (c *ResilientHTTPClient) ipFallbackTLSConfig(hostname string) *tls.Config {
	tlsCfg := &tls.Config{
		ServerName: hostname, // verify cert against original hostname
	}

	if len(c.caPEM) > 0 {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(c.caPEM)
		tlsCfg.RootCAs = pool
	}

	return tlsCfg
}

// HTTPClient returns the underlying http.Client for direct use.
func (c *ResilientHTTPClient) HTTPClient() *http.Client {
	return c.inner
}

// SetTransport replaces the transport on the inner client.
func (c *ResilientHTTPClient) SetTransport(t http.RoundTripper) {
	c.inner.Transport = t
}

// Get is a convenience method.
func (c *ResilientHTTPClient) Get(rawURL string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

// PostForm creates the URL from a string and does a form POST.
func (c *ResilientHTTPClient) PostForm(rawURL string, data url.Values) (*http.Response, error) {
	resp, err := c.inner.PostForm(rawURL, data)
	if err == nil {
		return resp, nil
	}
	// On failure, fall back via Do with proper request
	req, reqErr := http.NewRequest(http.MethodPost, rawURL, nil)
	if reqErr != nil {
		return nil, err
	}
	return c.Do(req)
}

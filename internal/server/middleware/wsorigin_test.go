package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckWSOrigin_NoOrigin(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	assert.True(t, CheckWSOrigin(req), "no origin header = non-browser client, allow")
}

func TestCheckWSOrigin_MatchingHost(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://example.com")
	assert.True(t, CheckWSOrigin(req))
}

func TestCheckWSOrigin_MismatchingHost(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://evil.com")
	assert.False(t, CheckWSOrigin(req))
}

func TestCheckWSOrigin_InvalidOrigin(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "://invalid")
	assert.False(t, CheckWSOrigin(req))
}

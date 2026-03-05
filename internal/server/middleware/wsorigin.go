package middleware

import (
	"net/http"
	"net/url"
)

// CheckWSOrigin validates that the WebSocket Origin header matches the request Host.
// Returns true if Origin is absent (non-browser clients) or matches Host.
func CheckWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser client
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

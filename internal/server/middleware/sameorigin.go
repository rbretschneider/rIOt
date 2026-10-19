package middleware

import (
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// originRefusedBody is the response body for every refusal, whatever the
	// reason, so a probing client learns nothing beyond "refused".
	originRefusedBody = `{"error":"origin_refused"}`

	// originLogInterval is the minimum gap between two refusal log lines for
	// the same client address.
	originLogInterval = time.Minute

	// originLogMaxEntries bounds the throttle table. A flood of distinct
	// client addresses cannot grow it past this many entries.
	originLogMaxEntries = 4096
)

// SameOrigin refuses state-changing requests (POST, PUT, PATCH, DELETE) that
// do not come from the application's own origin (GROWTH-001 AD-030, D-72).
//
// A request is accepted when Sec-Fetch-Site is "same-origin", or when Origin
// equals RequestScheme(r) + "://" + r.Host or one of allowedOrigins. Anything
// else, including a missing Origin, Origin: null and a Sec-Fetch-Site that is
// not same-origin without a matching Origin, gets 403 {"error":"origin_refused"}
// before the next handler runs, so a refused request has no side effect.
//
// When the request has a body, its media type must be application/json
// (parameters such as charset are allowed). Bodyless requests are allowed.
//
// Safe methods pass untouched. Only cookie-authenticated routes may use this:
// agent routes authenticate with a device key or a client certificate and are
// not subject to it.
//
// Refusals are logged at most once per client address per minute and never
// include header values.
func SameOrigin(allowedOrigins []string) func(http.Handler) http.Handler {
	return newSameOrigin(allowedOrigins, time.Now)
}

// newSameOrigin is SameOrigin with an injectable clock for the log throttle.
func newSameOrigin(allowedOrigins []string, now func() time.Time) func(http.Handler) http.Handler {
	allowed := make([]string, 0, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o = strings.TrimSpace(o); o != "" {
			allowed = append(allowed, o)
		}
	}
	throttle := &refusalLogThrottle{now: now, last: make(map[string]time.Time)}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isStateChanging(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			if !originAccepted(r, allowed) || !bodyIsJSONOrAbsent(r) {
				// One body for every refusal reason, and no header values in
				// the log: the headers are attacker-controlled input.
				throttle.log(r)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(originRefusedBody))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AdminAuthSameOrigin is AdminAuth preceded by the SameOrigin check. Every
// cookie-authenticated route mounts this chain (not a group or a shared route
// prefix), so the check follows the authentication and cannot reach the agent
// routes that share a path prefix with admin routes (GROWTH-001 SEC-032).
func AdminAuthSameOrigin(jwtSecret []byte, allowedOrigins []string) func(http.Handler) http.Handler {
	sameOrigin := SameOrigin(allowedOrigins)
	auth := AdminAuth(jwtSecret)
	return func(next http.Handler) http.Handler {
		return sameOrigin(auth(next))
	}
}

func isStateChanging(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// originAccepted reports whether the request demonstrably comes from the
// application's own origin or a configured allowed origin.
func originAccepted(r *http.Request, allowed []string) bool {
	// Browsers set Sec-Fetch-Site and scripts cannot forge it. It is checked
	// first because behind a TLS-terminating proxy that rewrites Host, Origin
	// never equals the scheme and Host rIOt sees, yet the dashboard must work.
	if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
		return true
	}
	// A missing Origin is refused: a client that sends neither header cannot
	// prove where it came from, and header-less scripts must opt in by sending
	// Origin. "null" is what sandboxed or redirected cross-site pages send.
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return false
	}
	// Case-insensitive because scheme and host are case-insensitive. Entries
	// in allowed are otherwise exact: no trailing slash, no default port,
	// because browsers send neither.
	if strings.EqualFold(origin, RequestScheme(r)+"://"+r.Host) {
		return true
	}
	for _, a := range allowed {
		if strings.EqualFold(origin, a) {
			return true
		}
	}
	return false
}

// bodyIsJSONOrAbsent is true for bodyless requests and for requests whose
// media type is application/json.
func bodyIsJSONOrAbsent(r *http.Request) bool {
	// Requiring JSON closes the form and text/plain route: a cross-site page
	// can send those without a CORS preflight. ContentLength is -1 for chunked
	// bodies, so only an exact 0 counts as "no body".
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}

// refusalLogThrottle allows one log line per client address per
// originLogInterval, in a table of at most originLogMaxEntries addresses.
type refusalLogThrottle struct {
	now  func() time.Time
	mu   sync.Mutex
	last map[string]time.Time
}

// log is throttled because a cross-origin flood would otherwise write one log
// line per request and grow the log without bound.
func (t *refusalLogThrottle) log(r *http.Request) {
	ip := ClientIP(r)
	if !t.allow(ip) {
		return
	}
	slog.Warn("origin refused", "client_ip", ip, "method", r.Method)
}

// allow records and reports whether a log line for ip may be written now.
func (t *refusalLogThrottle) allow(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	if prev, ok := t.last[ip]; ok && now.Sub(prev) < originLogInterval {
		return false
	}
	if _, ok := t.last[ip]; !ok && len(t.last) >= originLogMaxEntries {
		for k, v := range t.last {
			if now.Sub(v) >= originLogInterval {
				delete(t.last, k)
			}
		}
		if len(t.last) >= originLogMaxEntries {
			// Table full of live entries: drop the log line rather than grow.
			return false
		}
	}
	t.last[ip] = now
	return true
}

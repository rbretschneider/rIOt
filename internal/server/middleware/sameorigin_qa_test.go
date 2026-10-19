package middleware

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Added by QA Engineer (GROWTH-001 PR-D).
//
// The unit tests in sameorigin_test.go build requests with httptest.NewRequest.
// These tests send real bytes over a socket to a real net/http server, so the
// ContentLength and Body values the middleware sees are the ones net/http
// derives from the wire, not ones a test author set by hand.

// rawExchange sends raw to a SameOrigin-guarded server and returns the status
// code and body of the first response, plus how many times the guarded handler
// ran. trusted, when set, puts RealIP with that trusted-proxy list in front.
func rawExchange(t *testing.T, allowed []string, trusted string, raw string) (int, string, int) {
	t.Helper()
	next := &okHandler{}
	var mu sync.Mutex
	counted := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		next.ServeHTTP(w, r)
	})
	var h http.Handler = SameOrigin(allowed)(counted)
	if trusted != "" {
		h = RealIP(ParseTrustedProxies(trusted))(h)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	conn, err := net.DialTimeout("tcp", srv.Listener.Addr().String(), 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(3*time.Second)))
	_, err = io.WriteString(conn, raw)
	require.NoError(t, err)

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	mu.Lock()
	defer mu.Unlock()
	return resp.StatusCode, string(body), next.calls
}

func chunked(body string) string {
	return fmt.Sprintf("%x\r\n%s\r\n0\r\n\r\n", len(body), body)
}

// Covers AC D-72, SEC-032 item 4: a chunked request has no Content-Length, so
// the middleware must not treat it as bodyless.
func TestGROWTH_D72_ChunkedBodyCannotBypassJSONCheck(t *testing.T) {
	const payload = `{"a":1}`
	req := func(contentType, extra string) string {
		ct := ""
		if contentType != "" {
			ct = "Content-Type: " + contentType + "\r\n"
		}
		return "POST /api/v1/x HTTP/1.1\r\nHost: riot.test\r\nOrigin: http://riot.test\r\n" +
			ct + extra + "Transfer-Encoding: chunked\r\nConnection: close\r\n\r\n" + chunked(payload)
	}

	t.Run("chunked text/plain is refused", func(t *testing.T) {
		code, body, calls := rawExchange(t, nil, "", req("text/plain", ""))
		assert.Equal(t, http.StatusForbidden, code)
		assert.JSONEq(t, `{"error":"origin_refused"}`, body)
		assert.Zero(t, calls, "handler must not run")
	})
	t.Run("chunked with no Content-Type is refused", func(t *testing.T) {
		code, _, calls := rawExchange(t, nil, "", req("", ""))
		assert.Equal(t, http.StatusForbidden, code)
		assert.Zero(t, calls)
	})
	t.Run("chunked form-encoded is refused", func(t *testing.T) {
		code, _, calls := rawExchange(t, nil, "", req("application/x-www-form-urlencoded", ""))
		assert.Equal(t, http.StatusForbidden, code)
		assert.Zero(t, calls)
	})
	t.Run("chunked multipart is refused", func(t *testing.T) {
		code, _, calls := rawExchange(t, nil, "", req("multipart/form-data; boundary=x", ""))
		assert.Equal(t, http.StatusForbidden, code)
		assert.Zero(t, calls)
	})
	t.Run("Content-Length: 0 alongside chunked does not make it bodyless", func(t *testing.T) {
		// net/http lets Transfer-Encoding win and drops the Content-Length.
		code, _, calls := rawExchange(t, nil, "", req("text/plain", "Content-Length: 0\r\n"))
		assert.Equal(t, http.StatusForbidden, code)
		assert.Zero(t, calls)
	})
	t.Run("chunked application/json is accepted", func(t *testing.T) {
		code, _, calls := rawExchange(t, nil, "", req("application/json", ""))
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, 1, calls)
	})
	t.Run("chunked with a foreign Origin is refused whatever the type", func(t *testing.T) {
		raw := "POST /api/v1/x HTTP/1.1\r\nHost: riot.test\r\nOrigin: http://evil.example\r\n" +
			"Content-Type: application/json\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n" + chunked(payload)
		code, _, calls := rawExchange(t, nil, "", raw)
		assert.Equal(t, http.StatusForbidden, code)
		assert.Zero(t, calls)
	})
}

// Covers AC D-72: an explicit Content-Length: 0 is a genuinely empty body.
func TestGROWTH_D72_ZeroLengthBodyOverTheWire(t *testing.T) {
	raw := "POST /api/v1/x HTTP/1.1\r\nHost: riot.test\r\nOrigin: http://riot.test\r\n" +
		"Content-Type: text/plain\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
	code, _, calls := rawExchange(t, nil, "", raw)
	assert.Equal(t, http.StatusOK, code, "an empty body has nothing to misinterpret")
	assert.Equal(t, 1, calls)

	// A sized body is checked.
	raw = "POST /api/v1/x HTTP/1.1\r\nHost: riot.test\r\nOrigin: http://riot.test\r\n" +
		"Content-Type: text/plain\r\nContent-Length: 2\r\nConnection: close\r\n\r\nhi"
	code, _, calls = rawExchange(t, nil, "", raw)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Zero(t, calls)
}

// Covers AC D-72 (named in AD-030): application/json with parameters passes.
func TestGROWTH_D72_JSONWithCharsetAccepted(t *testing.T) {
	for _, ct := range []string{
		"application/json",
		"application/json; charset=utf-8",
		"application/json;charset=UTF-8",
		"APPLICATION/JSON ; charset=utf-8",
	} {
		raw := "POST /api/v1/x HTTP/1.1\r\nHost: riot.test\r\nOrigin: http://riot.test\r\n" +
			"Content-Type: " + ct + "\r\nContent-Length: 7\r\nConnection: close\r\n\r\n" + `{"a":1}`
		code, _, calls := rawExchange(t, nil, "", raw)
		assert.Equal(t, http.StatusOK, code, ct)
		assert.Equal(t, 1, calls, ct)
	}
}

// Covers SEC-032 item 5 (named in AD-030): a proxy rewrites Host and sets
// X-Forwarded-Proto; the dashboard's Sec-Fetch-Site: same-origin is accepted,
// and without it (and without an AllowedOrigins match) the request is 403.
func TestGROWTH_D72_BehindProxySecFetchSiteAccepted(t *testing.T) {
	proxied := func(extra string) string {
		return "POST /api/v1/x HTTP/1.1\r\nHost: riot-upstream:7331\r\nX-Forwarded-Proto: https\r\n" +
			"Origin: https://riot.example.com\r\n" + extra + "Connection: close\r\n\r\n"
	}
	code, _, calls := rawExchange(t, nil, "127.0.0.0/8", proxied("Sec-Fetch-Site: same-origin\r\n"))
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, 1, calls)

	code, _, calls = rawExchange(t, nil, "127.0.0.0/8", proxied(""))
	assert.Equal(t, http.StatusForbidden, code)
	assert.Zero(t, calls)

	code, _, calls = rawExchange(t, []string{"https://riot.example.com"}, "127.0.0.0/8", proxied(""))
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, 1, calls)
}

// Covers AC D-72, adversarial: the scheme comes only from a TRUSTED proxy. An
// untrusted client that sets X-Forwarded-Proto: https must not be able to make
// an https Origin match an http server.
func TestGROWTH_D72_UntrustedForwardedProtoDoesNotChangeOrigin(t *testing.T) {
	raw := "POST /api/v1/x HTTP/1.1\r\nHost: riot.test\r\nX-Forwarded-Proto: https\r\n" +
		"Origin: https://riot.test\r\nConnection: close\r\n\r\n"
	// The only trusted range is 10.0.0.0/8; the test client is 127.0.0.1.
	code, _, calls := rawExchange(t, nil, "10.0.0.0/8", raw)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Zero(t, calls)
}

// Covers AC D-72, adversarial: Origin spelling differences.
func TestGROWTH_D72_OriginSpellingVariants(t *testing.T) {
	cases := []struct {
		name, origin string
		allowed      []string
		host         string
		ok           bool
	}{
		{"exact", "http://riot.test", nil, "riot.test", true},
		{"upper-case scheme and host (case-insensitive origin)", "HTTP://RIOT.TEST", nil, "riot.test", true},
		{"trailing slash is not a valid Origin", "http://riot.test/", nil, "riot.test", false},
		{"path appended", "http://riot.test/x", nil, "riot.test", false},
		{"explicit default port is not what a browser sends", "http://riot.test:80", nil, "riot.test", false},
		{"matching non-default port", "http://riot.test:7331", nil, "riot.test:7331", true},
		{"wrong port", "http://riot.test:7332", nil, "riot.test:7331", false},
		{"userinfo prefix", "http://evil@riot.test", nil, "riot.test", false},
		{"suffix host (prefix attack)", "http://riot.test.evil.example", nil, "riot.test", false},
		{"subdomain of the host", "http://a.riot.test", nil, "riot.test", false},
		{"Host that merely contains the origin host", "http://riot.test", nil, "xriot.test", false},
		{"allowed origin, exact", "https://app.example.com", []string{"https://app.example.com"}, "riot.test", true},
		{"allowed origin with a trailing slash in config never matches", "https://app.example.com", []string{"https://app.example.com/"}, "riot.test", false},
		{"allowed origin is not a prefix match", "https://app.example.com.evil.example", []string{"https://app.example.com"}, "riot.test", false},
		{"allowed origin wildcard is not special", "https://x.example.com", []string{"*"}, "riot.test", false},
		{"empty Origin value", "", nil, "riot.test", false},
		{"whitespace-only Origin", " ", nil, "riot.test", false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://"+c.host+"/api/v1/x", nil)
			req.Host = c.host
			req.Header.Set("Origin", c.origin)
			rec, next := serveSameOrigin(c.allowed, req)
			if c.ok {
				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Equal(t, 1, next.calls)
			} else {
				assertRefused(t, rec, next, c.name)
			}
		})
	}
}

// Covers AC D-72: safe methods are never refused, state-changing ones always
// are without a proof of origin.
func TestGROWTH_D72_MethodMatrix(t *testing.T) {
	for _, m := range []string{"GET", "HEAD", "OPTIONS"} {
		rec, next := serveSameOrigin(nil, sameOriginReq(m, "", map[string]string{"Origin": "https://evil.example"}))
		assert.Equal(t, http.StatusOK, rec.Code, m)
		assert.Equal(t, 1, next.calls, m)
	}
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		rec, next := serveSameOrigin(nil, sameOriginReq(m, "", nil))
		assertRefused(t, rec, next, m)
	}
}

// Covers AC D-72, log bound: concurrent refusals from many addresses never push
// the throttle table past its cap and never admit more than one line per
// address per window.
func TestGROWTH_D72_RefusalLogThrottleConcurrent(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	th := &refusalLogThrottle{now: clock.now, last: map[string]time.Time{}}

	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := map[string]int{}
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < originLogMaxEntries; i++ {
				ip := fmt.Sprintf("ip-%d", i%(originLogMaxEntries*2))
				if th.allow(ip) {
					mu.Lock()
					allowed[ip]++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	for ip, n := range allowed {
		assert.Equal(t, 1, n, "%s logged more than once inside the window", ip)
	}
	assert.LessOrEqual(t, len(th.last), originLogMaxEntries)
}

// Covers AC D-72: the refusal body is fixed and never echoes request input.
func TestGROWTH_D72_RefusalBodyDoesNotEchoInput(t *testing.T) {
	rec, _ := serveSameOrigin(nil, sameOriginReq("POST", "", map[string]string{
		"Origin": "https://<script>alert(1)</script>.example", "Sec-Fetch-Site": "<b>x</b>",
	}))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.False(t, strings.ContainsAny(rec.Body.String(), "<>"), rec.Body.String())
	assert.Equal(t, `{"error":"origin_refused"}`, rec.Body.String())
}

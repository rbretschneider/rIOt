package middleware

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// okHandler records that the guarded handler ran, so a test can prove a
// refused request had no side effect.
type okHandler struct{ calls int }

func (h *okHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.calls++
	w.WriteHeader(http.StatusOK)
}

func sameOriginReq(method, body string, headers map[string]string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, "http://riot.test/api/v1/x", nil)
	} else {
		req = httptest.NewRequest(method, "http://riot.test/api/v1/x", strings.NewReader(body))
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func serveSameOrigin(allowed []string, req *http.Request) (*httptest.ResponseRecorder, *okHandler) {
	next := &okHandler{}
	rec := httptest.NewRecorder()
	SameOrigin(allowed)(next).ServeHTTP(rec, req)
	return rec, next
}

func assertRefused(t *testing.T, rec *httptest.ResponseRecorder, next *okHandler, what string) {
	t.Helper()
	assert.Equal(t, http.StatusForbidden, rec.Code, "%s: status", what)
	assert.JSONEq(t, `{"error":"origin_refused"}`, rec.Body.String(), "%s: body", what)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"), "%s: content type", what)
	assert.Zero(t, next.calls, "%s: the handler must not run for a refused request", what)
}

func TestGROWTH_D72_CrossOriginStateChangeRejected(t *testing.T) {
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		method := method
		t.Run(method+" with a foreign Origin", func(t *testing.T) {
			rec, next := serveSameOrigin(nil, sameOriginReq(method, "", map[string]string{"Origin": "https://evil.example"}))
			assertRefused(t, rec, next, method)
		})
		t.Run(method+" with no Origin and no Sec-Fetch-Site", func(t *testing.T) {
			rec, next := serveSameOrigin(nil, sameOriginReq(method, "", nil))
			assertRefused(t, rec, next, method)
		})
	}

	t.Run("Origin null is refused", func(t *testing.T) {
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", "", map[string]string{"Origin": "null"}))
		assertRefused(t, rec, next, "Origin: null")
	})
	t.Run("Origin null is refused even if the operator listed it", func(t *testing.T) {
		rec, next := serveSameOrigin([]string{"null"}, sameOriginReq("POST", "", map[string]string{"Origin": "null"}))
		assertRefused(t, rec, next, "Origin: null, listed")
	})
	t.Run("Sec-Fetch-Site cross-site with no matching Origin is refused", func(t *testing.T) {
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", "", map[string]string{"Sec-Fetch-Site": "cross-site"}))
		assertRefused(t, rec, next, "cross-site")
	})
	t.Run("Sec-Fetch-Site same-site with a foreign Origin is refused", func(t *testing.T) {
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", "", map[string]string{
			"Sec-Fetch-Site": "same-site", "Origin": "https://other.riot.test",
		}))
		assertRefused(t, rec, next, "same-site")
	})
	t.Run("Sec-Fetch-Site none is refused", func(t *testing.T) {
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", "", map[string]string{"Sec-Fetch-Site": "none"}))
		assertRefused(t, rec, next, "none")
	})
	t.Run("an Origin differing only by port is refused", func(t *testing.T) {
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", "", map[string]string{"Origin": "http://riot.test:9999"}))
		assertRefused(t, rec, next, "port mismatch")
	})
	t.Run("an Origin differing only by scheme is refused", func(t *testing.T) {
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", "", map[string]string{"Origin": "https://riot.test"}))
		assertRefused(t, rec, next, "scheme mismatch")
	})
}

func TestGROWTH_D72_SameOriginAccepted(t *testing.T) {
	t.Run("Origin equal to scheme and Host", func(t *testing.T) {
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", "", map[string]string{"Origin": "http://riot.test"}))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls, "handler must run")
	})
	t.Run("Sec-Fetch-Site same-origin without Origin", func(t *testing.T) {
		rec, next := serveSameOrigin(nil, sameOriginReq("DELETE", "", map[string]string{"Sec-Fetch-Site": "same-origin"}))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls, "handler must run")
	})
	t.Run("an Origin in AllowedOrigins", func(t *testing.T) {
		rec, next := serveSameOrigin([]string{"https://riot.example.com"},
			sameOriginReq("PUT", "", map[string]string{"Origin": "https://riot.example.com"}))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls, "handler must run")
	})
	t.Run("Sec-Fetch-Site cross-site with an AllowedOrigins match (existing CORS feature)", func(t *testing.T) {
		rec, next := serveSameOrigin([]string{"https://app.example.com"}, sameOriginReq("POST", "", map[string]string{
			"Sec-Fetch-Site": "cross-site", "Origin": "https://app.example.com",
		}))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls, "handler must run")
	})
	t.Run("GET passes with no headers", func(t *testing.T) {
		rec, next := serveSameOrigin(nil, sameOriginReq("GET", "", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls, "handler must run")
	})
	t.Run("GET passes with a foreign Origin", func(t *testing.T) {
		rec, next := serveSameOrigin(nil, sameOriginReq("GET", "", map[string]string{"Origin": "https://evil.example"}))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls, "handler must run")
	})
	t.Run("HEAD and OPTIONS pass", func(t *testing.T) {
		for _, m := range []string{"HEAD", "OPTIONS"} {
			rec, next := serveSameOrigin(nil, sameOriginReq(m, "", nil))
			assert.Equal(t, http.StatusOK, rec.Code, m)
			assert.Equal(t, 1, next.calls, m)
		}
	})
	t.Run("a trusted-proxy scheme is part of the request origin", func(t *testing.T) {
		next := &okHandler{}
		rec := httptest.NewRecorder()
		req := sameOriginReq("POST", "", map[string]string{"Origin": "https://riot.test", "X-Forwarded-Proto": "https"})
		req.RemoteAddr = "10.0.0.5:4000"
		chain := RealIP(ParseTrustedProxies("10.0.0.0/8"))(SameOrigin(nil)(next))
		chain.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls, "Origin https://host must match the trusted X-Forwarded-Proto scheme")
	})
}

func TestGROWTH_D72_TextPlainBodyRejected(t *testing.T) {
	sameOrigin := map[string]string{"Origin": "http://riot.test"}

	t.Run("text/plain body is refused", func(t *testing.T) {
		h := map[string]string{"Origin": "http://riot.test", "Content-Type": "text/plain"}
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", `{"a":1}`, h))
		assertRefused(t, rec, next, "text/plain")
	})
	t.Run("form-encoded body is refused", func(t *testing.T) {
		h := map[string]string{"Origin": "http://riot.test", "Content-Type": "application/x-www-form-urlencoded"}
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", "a=1", h))
		assertRefused(t, rec, next, "form")
	})
	t.Run("a body with no Content-Type is refused", func(t *testing.T) {
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", `{"a":1}`, sameOrigin))
		assertRefused(t, rec, next, "no content type")
	})
	t.Run("an unparsable Content-Type is refused", func(t *testing.T) {
		h := map[string]string{"Origin": "http://riot.test", "Content-Type": "application/json;;;="}
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", `{"a":1}`, h))
		assertRefused(t, rec, next, "unparsable")
	})
	t.Run("a lookalike media type is refused", func(t *testing.T) {
		h := map[string]string{"Origin": "http://riot.test", "Content-Type": "application/jsonx"}
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", `{"a":1}`, h))
		assertRefused(t, rec, next, "lookalike")
	})
	t.Run("application/json is accepted", func(t *testing.T) {
		h := map[string]string{"Origin": "http://riot.test", "Content-Type": "application/json"}
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", `{"a":1}`, h))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls)
	})
	t.Run("application/json; charset=utf-8 is accepted (SEC-032 item 4)", func(t *testing.T) {
		h := map[string]string{"Origin": "http://riot.test", "Content-Type": "application/json; charset=utf-8"}
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", `{"a":1}`, h))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls)
	})
	t.Run("a mixed-case media type is accepted", func(t *testing.T) {
		h := map[string]string{"Origin": "http://riot.test", "Content-Type": "Application/JSON"}
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", `{"a":1}`, h))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls)
	})
	t.Run("a bodyless request needs no Content-Type", func(t *testing.T) {
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", "", sameOrigin))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls)
	})
	t.Run("a bodyless request with a text/plain Content-Type is accepted", func(t *testing.T) {
		h := map[string]string{"Origin": "http://riot.test", "Content-Type": "text/plain"}
		rec, next := serveSameOrigin(nil, sameOriginReq("POST", "", h))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls)
	})
	t.Run("a chunked body (unknown length) is still checked", func(t *testing.T) {
		req := sameOriginReq("POST", `{"a":1}`, map[string]string{"Origin": "http://riot.test", "Content-Type": "text/plain"})
		req.ContentLength = -1
		rec, next := serveSameOrigin(nil, req)
		assertRefused(t, rec, next, "chunked")
	})
	t.Run("GET with a text/plain body is not checked", func(t *testing.T) {
		req := sameOriginReq("GET", `x`, map[string]string{"Content-Type": "text/plain"})
		rec, next := serveSameOrigin(nil, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls)
	})
}

// SEC-032 item 5: behind a reverse proxy the Host header is rewritten to the
// upstream address, so a browser Origin of the public name no longer equals
// scheme://Host.
func TestGROWTH_SEC032_BehindProxy(t *testing.T) {
	trusted := ParseTrustedProxies("10.0.0.0/8")
	proxied := func(headers map[string]string) *http.Request {
		req := sameOriginReq("POST", "", headers)
		req.Host = "riot-upstream:7331" // rewritten by the proxy
		req.RemoteAddr = "10.0.0.5:4000"
		req.Header.Set("X-Forwarded-Proto", "https")
		return req
	}
	serve := func(allowed []string, req *http.Request) (*httptest.ResponseRecorder, *okHandler) {
		next := &okHandler{}
		rec := httptest.NewRecorder()
		RealIP(trusted)(SameOrigin(allowed)(next)).ServeHTTP(rec, req)
		return rec, next
	}

	t.Run("Host rewritten, Sec-Fetch-Site same-origin is accepted", func(t *testing.T) {
		rec, next := serve(nil, proxied(map[string]string{
			"Sec-Fetch-Site": "same-origin", "Origin": "https://riot.example.com",
		}))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls)
	})
	t.Run("Host rewritten, neither Sec-Fetch-Site nor an AllowedOrigins match is 403", func(t *testing.T) {
		rec, next := serve(nil, proxied(map[string]string{"Origin": "https://riot.example.com"}))
		assertRefused(t, rec, next, "proxied, no match")
	})
	t.Run("Host rewritten, no headers at all is 403", func(t *testing.T) {
		rec, next := serve([]string{"https://riot.example.com"}, proxied(nil))
		assertRefused(t, rec, next, "proxied, no headers")
	})
	t.Run("Host rewritten, Origin in AllowedOrigins is accepted", func(t *testing.T) {
		rec, next := serve([]string{"https://riot.example.com"},
			proxied(map[string]string{"Origin": "https://riot.example.com"}))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 1, next.calls)
	})
}

// fakeClock is a manually advanced clock for the log-throttle tests.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func TestGROWTH_D72_RefusalLogThrottle(t *testing.T) {
	t.Run("one line per client address per minute", func(t *testing.T) {
		clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
		th := &refusalLogThrottle{now: clock.now, last: map[string]time.Time{}}

		assert.True(t, th.allow("192.0.2.1"), "first refusal is logged")
		assert.False(t, th.allow("192.0.2.1"), "second refusal inside the minute is not logged")
		assert.True(t, th.allow("192.0.2.2"), "another client address is logged")

		clock.t = clock.t.Add(originLogInterval - time.Second)
		assert.False(t, th.allow("192.0.2.1"), "still inside the minute")
		clock.t = clock.t.Add(time.Second)
		assert.True(t, th.allow("192.0.2.1"), "logged again once the minute has passed")
	})

	t.Run("the table never exceeds its cap", func(t *testing.T) {
		clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
		th := &refusalLogThrottle{now: clock.now, last: map[string]time.Time{}}

		for i := 0; i < originLogMaxEntries+500; i++ {
			th.allow(fmt.Sprintf("ip-%d", i))
		}
		assert.Len(t, th.last, originLogMaxEntries, "table must stay at the cap while all entries are live")
		assert.False(t, th.allow("ip-new"), "a new address is dropped (not logged) while the table is full of live entries")
		assert.Len(t, th.last, originLogMaxEntries, "a dropped address must not be stored")

		clock.t = clock.t.Add(originLogInterval)
		assert.True(t, th.allow("ip-new"), "expired entries are swept to make room")
		assert.Len(t, th.last, 1, "only the new address remains after the sweep")
	})

	t.Run("a refusal logs once, without header values", func(t *testing.T) {
		var buf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
		defer slog.SetDefault(prev)

		clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
		next := &okHandler{}
		h := newSameOrigin(nil, clock.now)(next)
		for i := 0; i < 3; i++ {
			req := sameOriginReq("POST", "", map[string]string{
				"Origin": "https://secret-origin.invalid", "Sec-Fetch-Site": "secret-sfs",
			})
			req.RemoteAddr = "192.0.2.9:1234"
			h.ServeHTTP(httptest.NewRecorder(), req)
		}

		require.Equal(t, 1, strings.Count(buf.String(), "origin refused"), "three refusals in a minute log one line: %s", buf.String())
		assert.Contains(t, buf.String(), "192.0.2.9")
		assert.NotContains(t, buf.String(), "secret-origin.invalid", "header values must never be logged")
		assert.NotContains(t, buf.String(), "secret-sfs", "header values must never be logged")
		assert.Zero(t, next.calls)
	})
}

func TestGROWTH_SEC032_AdminAuthSameOriginRunsOriginBeforeAuth(t *testing.T) {
	secret := []byte("test-secret")
	next := &okHandler{}
	chain := AdminAuthSameOrigin(secret, nil)(next)

	t.Run("cross-origin request with no cookie is 403, not 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, sameOriginReq("POST", "", map[string]string{"Origin": "https://evil.example"}))
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.JSONEq(t, `{"error":"origin_refused"}`, rec.Body.String())
	})
	t.Run("same-origin request with no cookie reaches AdminAuth and is 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, sameOriginReq("POST", "", map[string]string{"Origin": "http://riot.test"}))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
	t.Run("GET with no cookie is 401 (origin check does not apply to reads)", func(t *testing.T) {
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, sameOriginReq("GET", "", nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
	assert.Zero(t, next.calls, "the handler must not run without a valid session")
}

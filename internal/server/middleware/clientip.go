package middleware

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// TrustedProxies holds the set of CIDR prefixes that are trusted to supply
// forwarding headers (X-Forwarded-For, X-Real-IP, True-Client-IP,
// X-Forwarded-Proto). The zero value (nil/empty) trusts nobody (OIDC-001
// AD-020) — the safe default for rIOt's documented no-proxy deployment.
type TrustedProxies struct {
	prefixes []netip.Prefix
}

// ParseTrustedProxies parses a comma-separated list of CIDR blocks (bare IPs
// are accepted and treated as /32 or /128). An entry that fails to parse is
// dropped with a slog.Warn rather than aborting boot; if every entry is bad
// the result trusts nobody (fails closed by construction).
func ParseTrustedProxies(raw string) *TrustedProxies {
	tp := &TrustedProxies{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return tp
	}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, err := parsePrefixOrIP(entry)
		if err != nil {
			slog.Warn("dropping invalid RIOT_TRUSTED_PROXIES entry", "entry", entry, "error", err.Error())
			continue
		}
		tp.prefixes = append(tp.prefixes, prefix)
	}
	return tp
}

func parsePrefixOrIP(entry string) (netip.Prefix, error) {
	if strings.Contains(entry, "/") {
		return netip.ParsePrefix(entry)
	}
	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// trusts reports whether the given address falls inside any configured
// trusted prefix. A nil or empty TrustedProxies trusts nobody.
func (tp *TrustedProxies) trusts(addr netip.Addr) bool {
	if tp == nil {
		return false
	}
	for _, p := range tp.prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

type schemeContextKey struct{}

// RealIP replaces chi's unconditional chimw.RealIP. It resolves the client
// identity from the TCP peer unless the immediate peer is an explicitly
// trusted proxy, in which case forwarding headers are honoured (OIDC-001
// AD-020, SEC-002).
func RealIP(tp *TrustedProxies) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peerHost, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				peerHost = r.RemoteAddr
			}
			peerAddr, parseErr := netip.ParseAddr(peerHost)

			if parseErr != nil || !tp.trusts(peerAddr) {
				// Untrusted or unparsable peer: ignore every forwarding header.
				next.ServeHTTP(w, r)
				return
			}

			// Trusted peer: honour X-Forwarded-For (rightmost-untrusted walk)
			// and X-Forwarded-Proto.
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				if client := rightmostUntrusted(xff, tp); client != "" {
					r.RemoteAddr = net.JoinHostPort(client, "0")
				}
			} else if v := r.Header.Get("X-Real-IP"); v != "" {
				if a, err := netip.ParseAddr(strings.TrimSpace(v)); err == nil {
					r.RemoteAddr = net.JoinHostPort(a.String(), "0")
				}
			} else if v := r.Header.Get("True-Client-IP"); v != "" {
				if a, err := netip.ParseAddr(strings.TrimSpace(v)); err == nil {
					r.RemoteAddr = net.JoinHostPort(a.String(), "0")
				}
			}

			if proto := r.Header.Get("X-Forwarded-Proto"); proto == "http" || proto == "https" {
				ctx := context.WithValue(r.Context(), schemeContextKey{}, proto)
				r = r.WithContext(ctx)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// rightmostUntrusted walks a comma-separated X-Forwarded-For chain from right
// to left, skipping entries that are themselves trusted, and returns the
// first untrusted address found. If every entry in the chain is trusted
// (C5), it falls back to the empty string so the caller leaves r.RemoteAddr
// untouched (i.e. the resolved client remains the TCP peer) — the
// conservative answer, since there is no untrusted client to attribute
// the request to.
func rightmostUntrusted(xff string, tp *TrustedProxies) string {
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		if candidate == "" {
			continue
		}
		addr, err := netip.ParseAddr(candidate)
		if err != nil {
			continue
		}
		if tp.trusts(addr) {
			continue
		}
		return addr.String()
	}
	return ""
}

// ClientIP returns the resolved client address for the request — the TCP
// peer, or the trusted-proxy-derived address if RealIP rewrote it. Every
// consumer that needs "the client IP" must call this rather than parsing
// r.RemoteAddr or reading X-Forwarded-* directly (OIDC-001 AD-020).
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// RequestScheme returns the resolved request scheme: the trusted
// X-Forwarded-Proto value recorded by RealIP, else "https" when r.TLS is
// set, else "http". Every consumer that needs "the request scheme" must call
// this (OIDC-001 AD-009, AD-020).
func RequestScheme(r *http.Request) string {
	if v, ok := r.Context().Value(schemeContextKey{}).(string); ok && v != "" {
		return v
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

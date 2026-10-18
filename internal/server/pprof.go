package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"net/netip"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// pprofHandler serves the profiling endpoints relative to /debug/pprof.
// It is never mounted on the main router (HARDEN-002 AD-001): it is served
// only by the opt-in loopback listener started from RIOT_PPROF_ADDR.
func pprofHandler() http.Handler {
	mux := chi.NewMux()
	mux.HandleFunc("/", pprof.Index)
	mux.HandleFunc("/cmdline", pprof.Cmdline)
	mux.HandleFunc("/profile", pprof.Profile)
	mux.HandleFunc("/symbol", pprof.Symbol)
	mux.HandleFunc("/trace", pprof.Trace)
	mux.Handle("/heap", pprof.Handler("heap"))
	mux.Handle("/goroutine", pprof.Handler("goroutine"))
	mux.Handle("/allocs", pprof.Handler("allocs"))
	return mux
}

// newPprofMux is the dedicated handler for the loopback listener. It must
// never be nil and must never be replaced by the default mux, which the
// net/http/pprof import also registers handlers on.
func newPprofMux() http.Handler {
	r := chi.NewRouter()
	r.Mount("/debug/pprof", pprofHandler())
	return r
}

// pprofNotFound answers /debug/pprof and /debug/pprof/* on the main router.
// Without an explicit route the SPA catch-all would answer these paths with
// index.html and HTTP 200, which looks like "still exposed" to a scanner.
var pprofNotFound = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
})

// ParsePprofAddr validates a RIOT_PPROF_ADDR value. It is pure: an empty value
// means disabled and returns ("", nil). Otherwise the host must be a literal
// IPv4 address in 127.0.0.0/8 or the IPv6 loopback ::1 with no zone, and the
// port must be canonical decimal 1-65535. The returned address is built only
// from the parsed values, never from the raw string, so what was validated is
// exactly what gets bound.
func ParsePprofAddr(raw string) (addr string, err error) {
	if raw == "" {
		return "", nil
	}
	host, portStr, splitErr := net.SplitHostPort(raw)
	if splitErr != nil {
		var ae *net.AddrError
		if errors.As(splitErr, &ae) && ae.Err == "missing port in address" {
			return "", errors.New("missing port")
		}
		return "", errors.New("malformed")
	}
	if portStr == "" {
		return "", errors.New("missing port")
	}

	a, parseErr := netip.ParseAddr(host)
	if parseErr != nil {
		return "", errors.New("host must be a literal IP")
	}
	if a.Zone() != "" {
		return "", errors.New("host has a zone")
	}
	// Not simplified to a.IsLoopback(): that unmaps 4-in-6 and would accept
	// [::ffff:127.0.0.1], which is documented as rejected.
	if !(a.Is4() && a.IsLoopback()) && a != netip.IPv6Loopback() {
		return "", errors.New("host is not loopback")
	}

	if !canonicalPort(portStr) {
		return "", errors.New("port not canonical decimal")
	}
	port, convErr := strconv.Atoi(portStr)
	if convErr != nil || port < 1 || port > 65535 {
		return "", errors.New("port out of range")
	}
	return net.JoinHostPort(a.String(), strconv.Itoa(port)), nil
}

// canonicalPort reports whether s is 1-5 ASCII digits with no leading zero,
// sign or whitespace.
func canonicalPort(s string) bool {
	if len(s) < 1 || len(s) > 5 || s[0] < '1' || s[0] > '9' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// pprofServer is the running loopback listener. All methods are safe on a nil
// receiver, which means "pprof is disabled".
type pprofServer struct {
	srv *http.Server
	ln  net.Listener
}

// Addr returns the bound address, or "" when pprof is disabled.
func (p *pprofServer) Addr() string {
	if p == nil {
		return ""
	}
	return p.ln.Addr().String()
}

// Shutdown stops the listener. /profile and /trace stream for up to 30 s, so
// if the graceful shutdown does not finish before ctx expires the listener is
// closed forcibly rather than left bound.
func (p *pprofServer) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	err := p.srv.Shutdown(ctx)
	if err != nil {
		_ = p.srv.Close()
	}
	return err
}

// listenPprof binds addr, which must already be validated by ParsePprofAddr
// (a test may pass 127.0.0.1:0), and serves the pprof endpoints on it.
func listenPprof(addr string, log *slog.Logger) (*pprofServer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{
		Handler:           newPprofMux(),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: /profile and /trace stream for up to 30 s.
	}
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Error("pprof listener stopped", "error", serveErr.Error())
		}
	}()
	log.Warn("pprof profiling enabled on loopback listener", "addr", ln.Addr().String())
	return &pprofServer{srv: srv, ln: ln}, nil
}

// startPprof applies RIOT_PPROF_ADDR. It returns nil when pprof is disabled,
// the value is rejected, or the bind fails. It never panics and never returns
// an error: a pprof misconfiguration must not stop the main server.
func startPprof(raw string, log *slog.Logger) *pprofServer {
	addr, err := ParsePprofAddr(raw)
	if err != nil {
		log.Error("rejected RIOT_PPROF_ADDR; pprof disabled", "value", raw, "reason", err.Error())
		return nil
	}
	if addr == "" {
		return nil
	}
	p, err := listenPprof(addr, log)
	if err != nil {
		log.Error("pprof listener bind failed; pprof disabled", "addr", addr, "error", err.Error())
		return nil
	}
	return p
}

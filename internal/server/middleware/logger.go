package middleware

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type wrappedWriter struct {
	http.ResponseWriter
	status int
}

func (w *wrappedWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker so WebSocket upgrades work through the logger.
func (w *wrappedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not support hijacking")
}

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &wrappedWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.status,
			"duration", time.Since(start).String(),
			"remote", r.RemoteAddr,
		)
	})
}

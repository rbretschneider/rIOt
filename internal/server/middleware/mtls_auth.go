package middleware

import (
	"context"
	"net/http"

	"github.com/DesyncTheThird/rIOt/internal/server/db"
)

type deviceIDKey struct{}

// MTLSDeviceAuth is middleware that authenticates devices via client certificates.
// It extracts the device ID from the TLS peer certificate CN.
func MTLSDeviceAuth(devices db.DeviceRepository, caRepo db.CARepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
				http.Error(w, `{"error":"client certificate required"}`, http.StatusUnauthorized)
				return
			}

			cert := r.TLS.PeerCertificates[0]
			deviceID := cert.Subject.CommonName
			if deviceID == "" {
				http.Error(w, `{"error":"certificate has no CN"}`, http.StatusUnauthorized)
				return
			}

			// Check if cert is revoked
			serial := cert.SerialNumber.Text(16)
			revokedSerials, err := caRepo.ListRevokedSerials(r.Context())
			if err == nil {
				for _, rs := range revokedSerials {
					if rs == serial {
						http.Error(w, `{"error":"certificate revoked"}`, http.StatusUnauthorized)
						return
					}
				}
			}

			// Verify device exists
			_, err = devices.GetByID(r.Context(), deviceID)
			if err != nil {
				http.Error(w, `{"error":"device not found"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), deviceIDKey{}, deviceID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// DeviceIDFromMTLS extracts the device ID set by MTLSDeviceAuth middleware.
func DeviceIDFromMTLS(ctx context.Context) string {
	if v, ok := ctx.Value(deviceIDKey{}).(string); ok {
		return v
	}
	return ""
}

package middleware

import (
	"context"
	"net/http"

	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/go-chi/chi/v5"
)

type contextKey string

const DeviceIDKey contextKey = "device_id"

// DeviceAuth validates the X-rIOt-Key header and ensures it matches the device in the URL.
func DeviceAuth(repo *db.DeviceRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-rIOt-Key")
			if apiKey == "" {
				http.Error(w, `{"error":"missing X-rIOt-Key header"}`, http.StatusUnauthorized)
				return
			}

			deviceID, err := repo.LookupAPIKey(r.Context(), apiKey)
			if err != nil {
				http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
				return
			}

			// Verify the key matches the device in the URL
			urlDeviceID := chi.URLParam(r, "id")
			if urlDeviceID != "" && deviceID != urlDeviceID {
				http.Error(w, `{"error":"api key does not match device"}`, http.StatusForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), DeviceIDKey, deviceID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

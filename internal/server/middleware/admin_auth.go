package middleware

import (
	"context"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

const AdminContextKey contextKey = "admin"

// AdminAuth validates the riot_session JWT cookie.
func AdminAuth(jwtSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("riot_session")
			if err != nil || cookie.Value == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			token, err := jwt.Parse(cookie.Value, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return jwtSecret, nil
			})
			if err != nil || !token.Valid {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), AdminContextKey, true)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AdminAuthWS validates the riot_session JWT cookie for WebSocket upgrades.
// Returns true if valid, false otherwise (does not write a response).
func AdminAuthWS(r *http.Request, jwtSecret []byte) bool {
	cookie, err := r.Cookie("riot_session")
	if err != nil || cookie.Value == "" {
		return false
	}
	token, err := jwt.Parse(cookie.Value, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return jwtSecret, nil
	})
	return err == nil && token.Valid
}

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

var testSecret = []byte("test-jwt-secret")

func makeToken(t *testing.T, secret []byte, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString(secret)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAdminAuth_ValidToken(t *testing.T) {
	tokenStr := makeToken(t, testSecret, jwt.MapClaims{
		"sub": "admin",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	})

	handler := AdminAuth(testSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify admin context is set
		admin := r.Context().Value(AdminContextKey)
		assert.Equal(t, true, admin)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/devices", nil)
	req.AddCookie(&http.Cookie{Name: "riot_session", Value: tokenStr})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAdminAuth_ExpiredToken(t *testing.T) {
	tokenStr := makeToken(t, testSecret, jwt.MapClaims{
		"sub": "admin",
		"exp": time.Now().Add(-1 * time.Hour).Unix(),
	})

	handler := AdminAuth(testSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called with expired token")
	}))

	req := httptest.NewRequest("GET", "/api/v1/devices", nil)
	req.AddCookie(&http.Cookie{Name: "riot_session", Value: tokenStr})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAdminAuth_NoCookie(t *testing.T) {
	handler := AdminAuth(testSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called without cookie")
	}))

	req := httptest.NewRequest("GET", "/api/v1/devices", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAdminAuth_WrongSigningMethod(t *testing.T) {
	// Sign with a different method (none)
	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": "admin",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	})
	tokenStr, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

	handler := AdminAuth(testSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called with none signing method")
	}))

	req := httptest.NewRequest("GET", "/api/v1/devices", nil)
	req.AddCookie(&http.Cookie{Name: "riot_session", Value: tokenStr})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAdminAuthWS_Valid(t *testing.T) {
	tokenStr := makeToken(t, testSecret, jwt.MapClaims{
		"sub": "admin",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	})

	req := httptest.NewRequest("GET", "/ws", nil)
	req.AddCookie(&http.Cookie{Name: "riot_session", Value: tokenStr})

	assert.True(t, AdminAuthWS(req, testSecret))
}

func TestAdminAuthWS_NoCookie(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	assert.False(t, AdminAuthWS(req, testSecret))
}

func TestAdminAuthWS_Expired(t *testing.T) {
	tokenStr := makeToken(t, testSecret, jwt.MapClaims{
		"sub": "admin",
		"exp": time.Now().Add(-1 * time.Hour).Unix(),
	})

	req := httptest.NewRequest("GET", "/ws", nil)
	req.AddCookie(&http.Cookie{Name: "riot_session", Value: tokenStr})

	assert.False(t, AdminAuthWS(req, testSecret))
}

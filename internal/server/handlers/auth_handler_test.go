package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func newTestHandlers(t *testing.T) (*Handlers, *testutil.MockAdminRepo) {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	adminRepo := testutil.NewMockAdminRepo(string(hash))
	h := &Handlers{
		adminRepo: adminRepo,
		jwtSecret: []byte("test-jwt-secret"),
	}
	return h, adminRepo
}

func TestLogin_CorrectPassword(t *testing.T) {
	h, _ := newTestHandlers(t)

	body, _ := json.Marshal(map[string]string{"password": "correct-password"})
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	// Check that a cookie was set
	cookies := rec.Result().Cookies()
	require.NotEmpty(t, cookies)
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "riot_session" {
			sessionCookie = c
		}
	}
	require.NotNil(t, sessionCookie)
	assert.NotEmpty(t, sessionCookie.Value)
	assert.True(t, sessionCookie.HttpOnly)
}

func TestLogin_WrongPassword(t *testing.T) {
	h, _ := newTestHandlers(t)

	body, _ := json.Marshal(map[string]string{"password": "wrong-password"})
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLogin_EmptyPassword(t *testing.T) {
	h, _ := newTestHandlers(t)

	body, _ := json.Marshal(map[string]string{"password": ""})
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLogout(t *testing.T) {
	h, _ := newTestHandlers(t)

	req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	rec := httptest.NewRecorder()

	h.Logout(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	cookies := rec.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "riot_session" {
			assert.Equal(t, -1, c.MaxAge, "cookie should be cleared")
		}
	}
}

func TestAuthCheck_ValidToken(t *testing.T) {
	h, _ := newTestHandlers(t)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "admin",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	})
	tokenStr, _ := token.SignedString(h.jwtSecret)

	req := httptest.NewRequest("GET", "/api/v1/auth/check", nil)
	req.AddCookie(&http.Cookie{Name: "riot_session", Value: tokenStr})
	rec := httptest.NewRecorder()

	h.AuthCheck(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]bool
	json.NewDecoder(rec.Body).Decode(&resp)
	assert.True(t, resp["authenticated"])
}

func TestAuthCheck_ExpiredToken(t *testing.T) {
	h, _ := newTestHandlers(t)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "admin",
		"exp": time.Now().Add(-1 * time.Hour).Unix(),
	})
	tokenStr, _ := token.SignedString(h.jwtSecret)

	req := httptest.NewRequest("GET", "/api/v1/auth/check", nil)
	req.AddCookie(&http.Cookie{Name: "riot_session", Value: tokenStr})
	rec := httptest.NewRecorder()

	h.AuthCheck(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]bool
	json.NewDecoder(rec.Body).Decode(&resp)
	assert.False(t, resp["authenticated"])
}

func TestAuthCheck_NoCookie(t *testing.T) {
	h, _ := newTestHandlers(t)

	req := httptest.NewRequest("GET", "/api/v1/auth/check", nil)
	rec := httptest.NewRecorder()

	h.AuthCheck(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]bool
	json.NewDecoder(rec.Body).Decode(&resp)
	assert.False(t, resp["authenticated"])
}

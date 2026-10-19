package handlers

// Added by QA Engineer (HARDEN-002). Edge-case probes of the write-only
// notification secret contract and the health endpoints. They reuse the
// secretsHarness from settings_secrets_test.go.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/server/notify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers AC-029, AC-022: submitting the sentinel for EVERY registry secret of a
// channel that never had any secret stores "" and never the sentinel, for every
// known type and for an unknown type.
func TestHARDEN002_QA_AC029_SentinelForNeverSetFieldEveryType(t *testing.T) {
	types := []string{"smtp", "ntfy", "telegram", "discord", "slack", "pushover", "gotify", "webhook", "futurething"}
	for _, typ := range types {
		typ := typ
		t.Run(typ, func(t *testing.T) {
			s := newSecretsHarness(t)
			keys, known := notify.SecretKeys(typ)
			base := chCfg{"host": "h"}
			if !known {
				// Every key of an unknown type counts as secret, so "host" would too.
				keys = []string{"anything", "other"}
				base = chCfg{}
			}
			id := s.seed(typ, base)

			submitted := chCfg{}
			for k, v := range base {
				submitted[k] = v
			}
			for _, k := range keys {
				submitted[k] = sentinel
			}
			rec, body := s.put(id, chCfg{"name": "n", "type": typ, "enabled": true, "config": submitted})

			require.Equal(t, http.StatusOK, rec.Code, body)
			assertNoSentinelStored(t, s.stored(id).Config, typ)
			for _, k := range keys {
				assert.Equal(t, "", s.stored(id).Config[k], "%s.%s was never set, so the sentinel resolves to empty", typ, k)
			}
			assert.NotContains(t, body, "\"[rIOt-redacted]\"", "an unset secret must read as empty in the response, never as configured")
		})
	}
}

// Covers AC-029, AC-025: a type change must not carry any stored secret across,
// whether the new config omits the key, sends the sentinel, or sends a plain
// value for an unrelated key.
func TestHARDEN002_QA_AC029_TypeChangeInheritsNothing(t *testing.T) {
	t.Run("smtp to ntfy with secrets omitted", func(t *testing.T) {
		s := newSecretsHarness(t)
		id := s.seed("smtp", chCfg{"password": "SMTPSECRET", "host": "h"})
		rec, body := s.put(id, chCfg{"name": "n", "type": "ntfy", "enabled": true, "config": chCfg{"server_url": "https://ntfy.home"}})
		require.Equal(t, http.StatusOK, rec.Code, body)
		stored := s.stored(id).Config
		assert.NotContains(t, stored, "password", "the old type's secret must not be carried over")
		assert.NotContains(t, stored, "topic", "an omitted secret of the new type stays unset")
		assert.NotContains(t, body, "SMTPSECRET")
	})
	t.Run("webhook to discord keeps neither url nor headers", func(t *testing.T) {
		s := newSecretsHarness(t)
		id := s.seed("webhook", chCfg{"url": "https://w.example/WHSECRET", "headers": map[string]interface{}{"Authorization": "Bearer HDRSECRET"}})
		rec, body := s.put(id, chCfg{"name": "n", "type": "discord", "enabled": true, "config": chCfg{"webhook_url": sentinel, "url": sentinel, "headers": map[string]interface{}{"Authorization": sentinel}}})
		require.Equal(t, http.StatusOK, rec.Code, body)
		stored := s.stored(id).Config
		assert.Equal(t, "", stored["webhook_url"], "a Discord URL must not be seeded from a webhook URL")
		assertNoSentinelStored(t, stored, "webhook->discord")
		assert.NotContains(t, body, "WHSECRET")
		assert.NotContains(t, body, "HDRSECRET")
		for _, v := range stored {
			assert.NotEqual(t, "https://w.example/WHSECRET", v, "stored secret leaked across types")
			assert.NotEqual(t, "Bearer HDRSECRET", v)
		}
	})
	t.Run("discord to slack cannot rebind the stored webhook URL", func(t *testing.T) {
		s := newSecretsHarness(t)
		id := s.seed("discord", chCfg{"webhook_url": "https://discord.example/DSECRET"})
		rec, body := s.put(id, chCfg{"name": "n", "type": "slack", "enabled": true, "config": chCfg{"webhook_url": sentinel}})
		require.Equal(t, http.StatusOK, rec.Code, body)
		assert.Equal(t, "", s.stored(id).Config["webhook_url"], "same key name, different type: still no inheritance")
	})
}

// Covers AC-030: header edge cases around present, omitted, null, empty and
// brand-new names carrying the sentinel.
func TestHARDEN002_QA_AC030_WebhookHeaderEdgeCases(t *testing.T) {
	seed := func(s *secretsHarness) int64 {
		return s.seed("webhook", chCfg{"url": "https://h/x", "headers": map[string]interface{}{"Authorization": "Bearer A"}})
	}
	put := func(s *secretsHarness, id int64, config chCfg) (int, string) {
		rec, body := s.put(id, chCfg{"name": "w", "type": "webhook", "enabled": true, "config": config})
		return rec.Code, body
	}

	t.Run("explicit null headers keeps the stored headers", func(t *testing.T) {
		s := newSecretsHarness(t)
		id := seed(s)
		code, body := put(s, id, chCfg{"url": sentinel, "headers": nil})
		require.Equal(t, http.StatusOK, code, body)
		assert.Equal(t, map[string]interface{}{"Authorization": "Bearer A"}, s.stored(id).Config["headers"])
	})
	t.Run("empty map is the complete desired set and clears all headers", func(t *testing.T) {
		s := newSecretsHarness(t)
		id := seed(s)
		code, body := put(s, id, chCfg{"url": sentinel, "headers": map[string]interface{}{}})
		require.Equal(t, http.StatusOK, code, body)
		assert.Equal(t, map[string]interface{}{}, s.stored(id).Config["headers"])
		assert.Equal(t, "https://h/x", s.stored(id).Config["url"])
	})
	t.Run("sentinel for a header name that was never stored becomes empty", func(t *testing.T) {
		s := newSecretsHarness(t)
		id := seed(s)
		code, body := put(s, id, chCfg{"url": sentinel, "headers": map[string]interface{}{"Authorization": sentinel, "X-Never": sentinel}})
		require.Equal(t, http.StatusOK, code, body)
		stored := s.stored(id).Config["headers"].(map[string]interface{})
		assert.Equal(t, "Bearer A", stored["Authorization"])
		assert.Equal(t, "", stored["X-Never"], "no sentinel may be persisted as a header value")
		assertNoSentinelStored(t, s.stored(id).Config, "headers")
	})
	t.Run("a non-map headers value never persists the sentinel", func(t *testing.T) {
		s := newSecretsHarness(t)
		id := seed(s)
		code, body := put(s, id, chCfg{"url": sentinel, "headers": sentinel})
		require.Equal(t, http.StatusOK, code, body)
		assertNoSentinelStored(t, s.stored(id).Config, "non-map headers")
		assert.NotContains(t, body, "Bearer A")
		assert.Equal(t, map[string]interface{}{"Authorization": "Bearer A"}, s.stored(id).Config["headers"],
			"QA-2: a sentinel in place of the headers map keeps the stored headers")
	})
	t.Run("header values never appear in the create response", func(t *testing.T) {
		s := newSecretsHarness(t)
		rec, body := s.do("POST", "/api/v1/settings/notification-channels",
			chCfg{"name": "w", "type": "webhook", "enabled": true, "config": chCfg{"url": "https://h/CREATEURL", "headers": map[string]interface{}{"X-Key": "CREATEHDR"}}})
		require.Equal(t, http.StatusCreated, rec.Code, body)
		assert.NotContains(t, body, "CREATEHDR")
		assert.NotContains(t, body, "CREATEURL")
		assert.Equal(t, "CREATEHDR", s.repo.Channels[0].Config["headers"].(map[string]interface{})["X-Key"], "the repository keeps the cleartext")
	})
}

// Covers AC-032: every unusable id is rejected without a write. A non-numeric id
// is a 400 (unchanged), an unknown id is a 404, and neither reaches UpdateChannel.
func TestHARDEN002_QA_AC032_UnknownAndMalformedIDs(t *testing.T) {
	s := newSecretsHarness(t)
	s.seed("smtp", chCfg{"password": "P"})
	body := chCfg{"name": "x", "type": "smtp", "enabled": true, "config": chCfg{"password": "NEW"}}

	for _, id := range []string{"0", "-1", "2", "9223372036854775807"} {
		rec, b := s.do("PUT", "/api/v1/settings/notification-channels/"+id, body)
		assert.Equal(t, http.StatusNotFound, rec.Code, "id %s: %s", id, b)
	}
	rec, _ := s.do("PUT", "/api/v1/settings/notification-channels/abc", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "a non-numeric id keeps its 400")
	rec, _ = s.do("PUT", "/api/v1/settings/notification-channels/99999999999999999999", body)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "an overflowing id keeps its 400")

	assert.Equal(t, 0, s.repo.UpdateChannelCalls, "no unusable id may reach UpdateChannel")
	assert.Equal(t, "P", s.stored(1).Config["password"])
}

// Covers AC-032, AC-026: a malformed JSON body is rejected with a 400 before any
// lookup or write, so a bad payload can never blank a stored secret.
func TestHARDEN002_QA_AC032_MalformedBodyNeverWrites(t *testing.T) {
	s := newSecretsHarness(t)
	id := s.seed("smtp", chCfg{"password": "P"})

	rec, _ := s.do("PUT", "/api/v1/settings/notification-channels/"+strconv.FormatInt(id, 10), "not-an-object")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, 0, s.repo.UpdateChannelCalls)
	assert.Equal(t, "P", s.stored(id).Config["password"])
}

// Covers AC-019, AC-020: a repeated list never mutates stored state (the mock
// repository shares its maps, so an in-place redaction would corrupt it).
func TestHARDEN002_QA_AC019_ListNeverMutatesStoredSecrets(t *testing.T) {
	s := newSecretsHarness(t)
	id := s.seed("webhook", chCfg{"url": "https://w/URLSECRET", "headers": map[string]interface{}{"Authorization": "HDRSECRET"}})
	s.seed("ntfy", chCfg{"server_url": "https://alice:S3cret@ntfy.home", "topic": "t", "token": "TK"})

	for i := 0; i < 3; i++ {
		_, body := s.list()
		assert.NotContains(t, body, "URLSECRET")
		assert.NotContains(t, body, "HDRSECRET")
		assert.NotContains(t, body, "S3cret")
	}
	assert.Equal(t, "https://w/URLSECRET", s.stored(id).Config["url"])
	assert.Equal(t, "HDRSECRET", s.stored(id).Config["headers"].(map[string]interface{})["Authorization"])
	assert.Equal(t, "https://alice:S3cret@ntfy.home", s.stored(2).Config["server_url"])
	assert.Equal(t, "TK", s.stored(2).Config["token"])
}

// Covers AC-051 (NFR-005): with NO probe timeout configured, a database ping that
// hangs forever is cut off at the 2 s default on both endpoints.
func TestHARDEN002_QA_AC051_RealDefaultTimeoutBoundsHungPing(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about 4 s")
	}
	release := make(chan struct{})
	defer close(release)
	hang := fakeDBChecker(func(context.Context) bool {
		<-release // ignores its context entirely, like a wedged driver
		return true
	})
	for _, endpoint := range []string{"/health", "/readyz"} {
		hh := &HealthHandler{DB: hang, Migrated: migrated(true)}
		start := time.Now()
		r := serveHealth(t, hh, endpoint)
		elapsed := time.Since(start)

		assert.GreaterOrEqual(t, elapsed, 1900*time.Millisecond, "%s: must wait for the probe for about the 2 s bound, took %s", endpoint, elapsed)
		assert.Less(t, elapsed, 3*time.Second, "%s: NFR-005 response within 3 s, took %s", endpoint, elapsed)
		assert.Equal(t, http.StatusServiceUnavailable, r.code, endpoint)
		assert.Equal(t, false, r.fields["database"], endpoint)
	}
}

// Covers AC-051: a client that disconnects mid-probe (request context cancelled)
// still gets a well-formed 503 and the handler returns promptly.
func TestHARDEN002_QA_AC051_CancelledRequestContextReturnsPromptly(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	hang := fakeDBChecker(func(context.Context) bool { <-release; return true })
	hh := &HealthHandler{DB: hang, Migrated: migrated(true)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() { hh.Health(rec, req); close(done) }()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not return after the request context was cancelled")
	}
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

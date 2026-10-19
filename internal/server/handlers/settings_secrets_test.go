package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/notify"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sentinel = notify.RedactedSecret

type chCfg = map[string]interface{}

// secretsHarness wires the notification-channel routes onto a chi router.
type secretsHarness struct {
	t      *testing.T
	h      *Handlers
	repo   *testutil.MockNotifyRepo
	router http.Handler
}

func newSecretsHarness(t *testing.T) *secretsHarness {
	t.Helper()
	h, _, repo := newSettingsTestHandlers(t)
	h.dispatcher = notify.NewDispatcher(repo)
	r := chi.NewRouter()
	r.Get("/api/v1/settings/notification-channels", h.ListNotificationChannels)
	r.Post("/api/v1/settings/notification-channels", h.CreateNotificationChannel)
	r.Put("/api/v1/settings/notification-channels/{id}", h.UpdateNotificationChannel)
	r.Post("/api/v1/settings/notification-channels/{id}/test", h.TestNotificationChannel)
	return &secretsHarness{t: t, h: h, repo: repo, router: r}
}

// seed stores a channel directly in the mock repository and returns its id.
func (s *secretsHarness) seed(typ string, config chCfg) int64 {
	id := int64(len(s.repo.Channels) + 1)
	s.repo.Channels = append(s.repo.Channels, models.NotificationChannel{ID: id, Name: typ + "-chan", Type: typ, Enabled: true, Config: config})
	s.repo.NextChanID = id + 1
	return id
}

func (s *secretsHarness) do(method, path string, body interface{}) (*httptest.ResponseRecorder, string) {
	s.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(s.t, err)
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec, rec.Body.String()
}

func (s *secretsHarness) list() ([]models.NotificationChannel, string) {
	s.t.Helper()
	rec, body := s.do("GET", "/api/v1/settings/notification-channels", nil)
	require.Equal(s.t, http.StatusOK, rec.Code, body)
	var out []models.NotificationChannel
	require.NoError(s.t, json.Unmarshal(rec.Body.Bytes(), &out), body)
	return out, body
}

func (s *secretsHarness) put(id int64, body interface{}) (*httptest.ResponseRecorder, string) {
	return s.do("PUT", "/api/v1/settings/notification-channels/"+strconv.FormatInt(id, 10), body)
}

func (s *secretsHarness) stored(id int64) models.NotificationChannel {
	s.t.Helper()
	for _, ch := range s.repo.Channels {
		if ch.ID == id {
			return ch
		}
	}
	s.t.Fatalf("channel %d not in repository", id)
	return models.NotificationChannel{}
}

func respConfig(t *testing.T, body string) chCfg {
	t.Helper()
	var ch models.NotificationChannel
	require.NoError(t, json.Unmarshal([]byte(body), &ch), body)
	return ch.Config
}

// assertNoSentinelStored fails if any stored string equals the sentinel or
// carries it as URL userinfo, at the top level or one level down.
func assertNoSentinelStored(t *testing.T, config chCfg, what string) {
	t.Helper()
	check := func(path string, v interface{}) {
		if s, ok := v.(string); ok {
			assert.NotEqual(t, sentinel, s, "%s: %s equals the sentinel", what, path)
			assert.False(t, strings.Contains(s, "://"+sentinel+"@"), "%s: %s carries the sentinel as userinfo: %q", what, path, s)
		}
	}
	for k, v := range config {
		check(k, v)
		if m, ok := v.(map[string]interface{}); ok {
			for k2, v2 := range m {
				check(k+"."+k2, v2)
			}
		}
	}
}

func TestHARDEN002_AC019_SMTPPasswordRedactedOnList(t *testing.T) {
	s := newSecretsHarness(t)
	s.seed("smtp", chCfg{"password": "hunter2-SECRET", "host": "mail.example", "username": "alerts"})

	channels, body := s.list()

	require.Len(t, channels, 1)
	assert.Equal(t, sentinel, channels[0].Config["password"], "password must be the sentinel")
	assert.Equal(t, "mail.example", channels[0].Config["host"])
	assert.Equal(t, "alerts", channels[0].Config["username"])
	assert.NotContains(t, body, "hunter2-SECRET", "the raw body must not contain the password")
}

func TestHARDEN002_AC019_C1_URLUserinfoRedactedOnList(t *testing.T) {
	s := newSecretsHarness(t)
	s.seed("ntfy", chCfg{"server_url": "https://alice:S3cret@ntfy.home", "topic": "t"})
	s.seed("gotify", chCfg{"server_url": "https://bob:Pw2@gotify.home", "app_token": "tok"})

	channels, body := s.list()

	for _, leak := range []string{"S3cret", "Pw2", "alice:", "bob:"} {
		assert.NotContains(t, body, leak, "the body must not contain %q", leak)
	}
	require.Len(t, channels, 2)
	assert.Equal(t, "https://[rIOt-redacted]@ntfy.home", channels[0].Config["server_url"], "host stays visible")
	assert.Equal(t, "https://[rIOt-redacted]@gotify.home", channels[1].Config["server_url"])
}

func TestHARDEN002_AC020_EveryRegistryTypeRedacted(t *testing.T) {
	s := newSecretsHarness(t)
	type row struct {
		typ     string
		config  chCfg
		secrets map[string]string
		plain   chCfg
	}
	rows := []row{
		{"smtp", chCfg{"password": "CANARY-1", "host": "mail.example", "port": float64(587), "username": "u", "from": "f@x", "to": "t@x", "starttls": true},
			map[string]string{"password": "CANARY-1"}, chCfg{"host": "mail.example", "port": float64(587), "username": "u", "from": "f@x", "to": "t@x", "starttls": true}},
		{"ntfy", chCfg{"token": "CANARY-2", "topic": "CANARY-3", "server_url": "https://ntfy.example", "priority": "high", "priority_map": map[string]interface{}{"info": "low"}},
			map[string]string{"token": "CANARY-2", "topic": "CANARY-3"}, chCfg{"server_url": "https://ntfy.example", "priority": "high", "priority_map": map[string]interface{}{"info": "low"}}},
		{"telegram", chCfg{"bot_token": "CANARY-4", "chat_id": "42"}, map[string]string{"bot_token": "CANARY-4"}, chCfg{"chat_id": "42"}},
		{"discord", chCfg{"webhook_url": "https://d.example/CANARY-5"}, map[string]string{"webhook_url": "https://d.example/CANARY-5"}, chCfg{}},
		{"slack", chCfg{"webhook_url": "https://s.example/CANARY-6"}, map[string]string{"webhook_url": "https://s.example/CANARY-6"}, chCfg{}},
		{"pushover", chCfg{"api_token": "CANARY-7", "user_key": "CANARY-8", "device": "phone"}, map[string]string{"api_token": "CANARY-7", "user_key": "CANARY-8"}, chCfg{"device": "phone"}},
		{"gotify", chCfg{"app_token": "CANARY-9", "server_url": "https://g.example"}, map[string]string{"app_token": "CANARY-9"}, chCfg{"server_url": "https://g.example"}},
		{"webhook", chCfg{"url": "https://w.example/CANARY-10", "headers": map[string]interface{}{"Authorization": "CANARY-11"}}, map[string]string{"url": "https://w.example/CANARY-10"}, chCfg{}},
	}
	for _, r := range rows {
		s.seed(r.typ, r.config)
	}

	channels, body := s.list()

	require.Len(t, channels, len(rows))
	for i, r := range rows {
		for k := range r.secrets {
			assert.Equal(t, sentinel, channels[i].Config[k], "%s.%s must be the sentinel", r.typ, k)
		}
		for k, v := range r.plain {
			assert.Equal(t, v, channels[i].Config[k], "%s.%s is non-secret and must be unchanged", r.typ, k)
		}
	}
	for n := 1; n <= 11; n++ {
		assert.NotContains(t, body, "CANARY-"+strconv.Itoa(n), "canary %d leaked into the response body", n)
	}
}

func TestHARDEN002_AC021_WebhookURLAndHeaderValuesRedacted(t *testing.T) {
	s := newSecretsHarness(t)
	s.seed("webhook", chCfg{"url": "https://hook.example/T0KEN", "headers": map[string]interface{}{"Authorization": "Bearer abc123", "X-Env": "prod"}})

	channels, body := s.list()

	require.Len(t, channels, 1)
	assert.Equal(t, sentinel, channels[0].Config["url"])
	assert.Equal(t, map[string]interface{}{"Authorization": sentinel, "X-Env": sentinel}, channels[0].Config["headers"],
		"headers must have exactly the two names, each with the sentinel")
	assert.NotContains(t, body, "T0KEN")
	assert.NotContains(t, body, "abc123")
}

func TestHARDEN002_AC022_UnsetSecretsNotSentinel(t *testing.T) {
	s := newSecretsHarness(t)
	s.seed("ntfy", chCfg{"topic": "t"})
	s.seed("ntfy", chCfg{"topic": "t", "token": ""})

	channels, _ := s.list()

	require.Len(t, channels, 2)
	for i, ch := range channels {
		tok, present := ch.Config["token"]
		if present {
			assert.Equal(t, "", tok, "channel %d: an unset token must read as empty, never the sentinel", i)
		}
	}
}

func TestHARDEN002_AC023_UnknownTypeFullyRedacted(t *testing.T) {
	s := newSecretsHarness(t)
	s.seed("futurething", chCfg{"a": "x1", "b": "y2"})

	channels, body := s.list()

	require.Len(t, channels, 1)
	assert.Equal(t, sentinel, channels[0].Config["a"])
	assert.Equal(t, sentinel, channels[0].Config["b"])
	assert.NotContains(t, body, "x1")
	assert.NotContains(t, body, "y2")
}

func TestHARDEN002_AC024_CreateStoresCleartextReturnsRedacted(t *testing.T) {
	s := newSecretsHarness(t)

	rec, body := s.do("POST", "/api/v1/settings/notification-channels",
		chCfg{"name": "tg", "type": "telegram", "enabled": true, "config": chCfg{"bot_token": "123:ABC", "chat_id": "42"}})

	require.Equal(t, http.StatusCreated, rec.Code, body)
	require.Len(t, s.repo.Channels, 1)
	assert.Equal(t, "123:ABC", s.repo.Channels[0].Config["bot_token"], "the repository must receive the cleartext")
	cfgOut := respConfig(t, body)
	assert.Equal(t, sentinel, cfgOut["bot_token"], "the response must show the sentinel")
	assert.Equal(t, "42", cfgOut["chat_id"])
	assert.NotContains(t, body, "123:ABC", "the cleartext must not be in the response body")
}

func TestHARDEN002_AC024_C1_CreateAndUpdateResponsesRedactUserinfo(t *testing.T) {
	s := newSecretsHarness(t)

	rec, body := s.do("POST", "/api/v1/settings/notification-channels",
		chCfg{"name": "nt", "type": "ntfy", "enabled": true, "config": chCfg{"server_url": "https://alice:S3cret@ntfy.home", "topic": "t"}})
	require.Equal(t, http.StatusCreated, rec.Code, body)
	assert.NotContains(t, body, "S3cret", "POST response must not carry the password")
	assert.Equal(t, "https://alice:S3cret@ntfy.home", s.repo.Channels[0].Config["server_url"], "the repository keeps the cleartext")

	rec, body = s.put(s.repo.Channels[0].ID,
		chCfg{"name": "nt2", "type": "ntfy", "enabled": true, "config": chCfg{"server_url": "https://[rIOt-redacted]@ntfy.home", "topic": sentinel}})
	require.Equal(t, http.StatusOK, rec.Code, body)
	assert.NotContains(t, body, "S3cret", "PUT response must not carry the password")
	assert.Equal(t, "https://alice:S3cret@ntfy.home", s.stored(1).Config["server_url"], "the repository still holds the cleartext")
}

func TestHARDEN002_AC025_PutSentinelKeepsSecret(t *testing.T) {
	s := newSecretsHarness(t)
	id := s.seed("smtp", chCfg{"password": "P1", "host": "h"})

	rec, body := s.put(id, chCfg{"name": "renamed", "type": "smtp", "enabled": true, "config": chCfg{"password": sentinel, "host": "h"}})

	require.Equal(t, http.StatusOK, rec.Code, body)
	assert.Equal(t, "renamed", s.stored(id).Name)
	assert.Equal(t, "P1", s.stored(id).Config["password"], "the sentinel must keep the stored secret")
	assert.Equal(t, sentinel, respConfig(t, body)["password"])
	assert.NotContains(t, body, "P1\"", "the response must not carry the secret")
}

func TestHARDEN002_AC025_C1_NameOnlyPutPreservesUserinfo(t *testing.T) {
	s := newSecretsHarness(t)
	id := s.seed("ntfy", chCfg{"server_url": "https://alice:S3cret@ntfy.home", "topic": "t"})

	listed, _ := s.list()
	require.Equal(t, "https://[rIOt-redacted]@ntfy.home", listed[0].Config["server_url"])

	rec, body := s.put(id, chCfg{"name": "only-the-name-changed", "type": "ntfy", "enabled": true, "config": listed[0].Config})

	require.Equal(t, http.StatusOK, rec.Code, body)
	assert.Equal(t, "https://alice:S3cret@ntfy.home", s.stored(id).Config["server_url"], "the stored credential must survive a name-only edit")
	assert.Equal(t, "t", s.stored(id).Config["topic"], "the topic round-trips through the sentinel")
	assert.Equal(t, "only-the-name-changed", s.stored(id).Name)
}

func TestHARDEN002_AC026_PutOmittedKeepsSecret(t *testing.T) {
	s := newSecretsHarness(t)
	id := s.seed("discord", chCfg{"webhook_url": "https://discord.example/W1"})

	rec, body := s.put(id, chCfg{"name": "d", "type": "discord", "enabled": true, "config": chCfg{}})

	require.Equal(t, http.StatusOK, rec.Code, body)
	assert.Equal(t, "https://discord.example/W1", s.stored(id).Config["webhook_url"])
}

func TestHARDEN002_AC027_PutNewValueReplaces(t *testing.T) {
	s := newSecretsHarness(t)
	id := s.seed("gotify", chCfg{"app_token": "OLD", "server_url": "https://g.example"})

	rec, body := s.put(id, chCfg{"name": "g", "type": "gotify", "enabled": true, "config": chCfg{"app_token": "NEW", "server_url": "https://g.example"}})

	require.Equal(t, http.StatusOK, rec.Code, body)
	assert.Equal(t, "NEW", s.stored(id).Config["app_token"])
	assert.Equal(t, sentinel, respConfig(t, body)["app_token"])
	assert.NotContains(t, body, "NEW")
}

func TestHARDEN002_AC028_PutEmptyClears(t *testing.T) {
	s := newSecretsHarness(t)
	id := s.seed("ntfy", chCfg{"token": "tk", "topic": "t"})

	rec, body := s.put(id, chCfg{"name": "n", "type": "ntfy", "enabled": true, "config": chCfg{"token": "", "topic": sentinel}})

	require.Equal(t, http.StatusOK, rec.Code, body)
	assert.Equal(t, "", s.stored(id).Config["token"], "an explicit empty string clears the secret")
	assert.Equal(t, "", respConfig(t, body)["token"], "the response shows empty, not the sentinel")
	assert.Equal(t, "t", s.stored(id).Config["topic"])
}

func TestHARDEN002_AC029_SentinelNeverPersisted(t *testing.T) {
	t.Run("a: nothing stored", func(t *testing.T) {
		s := newSecretsHarness(t)
		id := s.seed("smtp", chCfg{"host": "h"})
		rec, body := s.put(id, chCfg{"name": "s", "type": "smtp", "enabled": true, "config": chCfg{"host": "h", "password": sentinel}})
		require.Equal(t, http.StatusOK, rec.Code, body)
		assert.Equal(t, "", s.stored(id).Config["password"])
		assertNoSentinelStored(t, s.stored(id).Config, "a")
	})
	t.Run("b: type change", func(t *testing.T) {
		s := newSecretsHarness(t)
		id := s.seed("smtp", chCfg{"password": "P1"})
		rec, body := s.put(id, chCfg{"name": "s", "type": "pushover", "enabled": true, "config": chCfg{"api_token": sentinel}})
		require.Equal(t, http.StatusOK, rec.Code, body)
		assert.Equal(t, "", s.stored(id).Config["api_token"], "no inheritance across a type change")
		assertNoSentinelStored(t, s.stored(id).Config, "b")
	})
	t.Run("c: create", func(t *testing.T) {
		s := newSecretsHarness(t)
		rec, body := s.do("POST", "/api/v1/settings/notification-channels",
			chCfg{"name": "t", "type": "telegram", "enabled": true, "config": chCfg{"bot_token": sentinel, "chat_id": "1"}})
		require.Equal(t, http.StatusCreated, rec.Code, body)
		assert.Equal(t, "", s.repo.Channels[0].Config["bot_token"])
		assertNoSentinelStored(t, s.repo.Channels[0].Config, "c")
	})
}

func TestHARDEN002_AC029_C1_UserinfoSentinelNeverPersisted(t *testing.T) {
	const sentinelURL = "https://[rIOt-redacted]@ntfy.home"

	t.Run("a: stored value has no userinfo", func(t *testing.T) {
		s := newSecretsHarness(t)
		id := s.seed("ntfy", chCfg{"server_url": "https://ntfy.home", "topic": "t"})
		rec, body := s.put(id, chCfg{"name": "n", "type": "ntfy", "enabled": true, "config": chCfg{"server_url": sentinelURL, "topic": sentinel}})
		require.Equal(t, http.StatusOK, rec.Code, body)
		assert.Equal(t, "https://ntfy.home", s.stored(id).Config["server_url"])
		assertNoSentinelStored(t, s.stored(id).Config, "a")
	})
	t.Run("b: create", func(t *testing.T) {
		s := newSecretsHarness(t)
		rec, body := s.do("POST", "/api/v1/settings/notification-channels",
			chCfg{"name": "n", "type": "ntfy", "enabled": true, "config": chCfg{"server_url": sentinelURL, "topic": "t"}})
		require.Equal(t, http.StatusCreated, rec.Code, body)
		assert.Equal(t, "https://ntfy.home", s.repo.Channels[0].Config["server_url"])
		assertNoSentinelStored(t, s.repo.Channels[0].Config, "b")
	})
	t.Run("c: type change", func(t *testing.T) {
		s := newSecretsHarness(t)
		id := s.seed("gotify", chCfg{"server_url": "https://bob:Pw2@gotify.home", "app_token": "tok"})
		rec, body := s.put(id, chCfg{"name": "n", "type": "ntfy", "enabled": true, "config": chCfg{"server_url": "https://[rIOt-redacted]@gotify.home", "topic": "t"}})
		require.Equal(t, http.StatusOK, rec.Code, body)
		assert.Equal(t, "https://gotify.home", s.stored(id).Config["server_url"], "a type change inherits no credential")
		assertNoSentinelStored(t, s.stored(id).Config, "c")
	})
}

func TestHARDEN002_AC030_WebhookHeaderMerge(t *testing.T) {
	seedHooks := func(s *secretsHarness) int64 {
		return s.seed("webhook", chCfg{"url": "https://h/x", "headers": map[string]interface{}{"Authorization": "Bearer A", "X-Old": "o"}})
	}

	t.Run("a: omitted headers keep stored headers", func(t *testing.T) {
		s := newSecretsHarness(t)
		id := seedHooks(s)
		rec, body := s.put(id, chCfg{"name": "w", "type": "webhook", "enabled": true, "config": chCfg{"url": sentinel}})
		require.Equal(t, http.StatusOK, rec.Code, body)
		assert.Equal(t, map[string]interface{}{"Authorization": "Bearer A", "X-Old": "o"}, s.stored(id).Config["headers"])
		assert.Equal(t, "https://h/x", s.stored(id).Config["url"])
	})
	t.Run("b: present headers are the complete set", func(t *testing.T) {
		s := newSecretsHarness(t)
		id := seedHooks(s)
		rec, body := s.put(id, chCfg{"name": "w", "type": "webhook", "enabled": true,
			"config": chCfg{"url": sentinel, "headers": map[string]interface{}{"Authorization": sentinel, "X-New": "n"}}})
		require.Equal(t, http.StatusOK, rec.Code, body)
		assert.Equal(t, map[string]interface{}{"Authorization": "Bearer A", "X-New": "n"}, s.stored(id).Config["headers"], "X-Old must be removed")
		assert.NotContains(t, body, "Bearer A")
	})
}

func TestHARDEN002_AC031_NonSecretKeysReplaced(t *testing.T) {
	s := newSecretsHarness(t)
	id := s.seed("smtp", chCfg{"host": "h1", "from": "a@x", "password": "P"})

	rec, body := s.put(id, chCfg{"name": "s", "type": "smtp", "enabled": true, "config": chCfg{"host": "h2", "password": sentinel}})

	require.Equal(t, http.StatusOK, rec.Code, body)
	assert.Equal(t, chCfg{"host": "h2", "password": "P"}, s.stored(id).Config, "host replaced, password kept, from gone")
}

func TestHARDEN002_AC032_PutUnknownChannel404(t *testing.T) {
	s := newSecretsHarness(t)
	s.seed("smtp", chCfg{"password": "P"})

	rec, body := s.put(9999, chCfg{"name": "ghost", "type": "smtp", "enabled": true, "config": chCfg{"password": "NEW"}})

	assert.Equal(t, http.StatusNotFound, rec.Code, body)
	assert.JSONEq(t, `{"error":"channel not found"}`, body)
	assert.Equal(t, 0, s.repo.UpdateChannelCalls, "an unknown id must not reach UpdateChannel")
	assert.Equal(t, "P", s.stored(1).Config["password"], "other channels are untouched")
}

func TestHARDEN002_AC032_PutRepositoryErrorOnLookupIs404WithoutWrite(t *testing.T) {
	s := newSecretsHarness(t)
	s.seed("smtp", chCfg{"password": "P"})
	s.repo.Err = errors.New("db down")

	rec, _ := s.put(1, chCfg{"name": "x", "type": "smtp", "enabled": true, "config": chCfg{}})

	assert.Equal(t, http.StatusNotFound, rec.Code, "any GetChannel error is a 404 with no write")
	assert.Equal(t, 0, s.repo.UpdateChannelCalls)
}

// capturingTransport records the URL of every request and answers 200.
type capturingTransport struct{ urls []string }

func (c *capturingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.urls = append(c.urls, r.URL.String())
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
}

func TestHARDEN002_AC033_TestSendUsesStoredSecretAfterRedactedEdit(t *testing.T) {
	s := newSecretsHarness(t)
	id := s.seed("slack", chCfg{"webhook_url": "https://slack.example/S1"})
	ct := &capturingTransport{}
	s.h.dispatcher.SetHTTPClient(&http.Client{Transport: ct})

	rec, body := s.put(id, chCfg{"name": "renamed", "type": "slack", "enabled": true, "config": chCfg{"webhook_url": sentinel}})
	require.Equal(t, http.StatusOK, rec.Code, body)

	rec, body = s.do("POST", "/api/v1/settings/notification-channels/"+strconv.FormatInt(id, 10)+"/test", nil)

	require.Equal(t, http.StatusOK, rec.Code, body)
	assert.JSONEq(t, `{"success":true}`, body)
	assert.Equal(t, []string{"https://slack.example/S1"}, ct.urls, "the stub must receive the stored, unredacted URL")
}

func TestHARDEN002_AC034_TestSendErrorScrubbed(t *testing.T) {
	s := newSecretsHarness(t)
	id := s.seed("telegram", chCfg{"bot_token": "999:SECRETTOKEN", "chat_id": "1"})
	s.h.dispatcher.SetHTTPClient(&http.Client{Transport: failingTransport{}})

	rec, body := s.do("POST", "/api/v1/settings/notification-channels/"+strconv.FormatInt(id, 10)+"/test", nil)

	require.Equal(t, http.StatusOK, rec.Code, body)
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), body)
	assert.False(t, resp.Success)
	assert.NotContains(t, resp.Error, "999:SECRETTOKEN", "the error must not contain the bot token")
	assert.NotContains(t, resp.Error, "SECRETTOKEN")
	assert.Contains(t, resp.Error, sentinel, "the sentinel replaces the token")
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial tcp 192.0.2.1:443: connect: connection refused")
}

// AC-034 (C2): the test-send endpoint, through a real dispatcher and a real
// http.Client failing at the transport, scrubs every backend's secrets.
func TestHARDEN002_AC034_C2_TestSendRealClientErrorsScrubbed(t *testing.T) {
	rows := []struct {
		typ         string
		config      chCfg
		mustNotLeak []string
	}{
		{"telegram", chCfg{"bot_token": "999:TGTOKEN", "chat_id": "42"}, []string{"TGTOKEN"}},
		{"discord", chCfg{"webhook_url": "https://u1:DPASS@discord.example/api/webhooks/1/DTOK"}, []string{"DTOK", "DPASS"}},
		{"slack", chCfg{"webhook_url": "https://hooks.slack.example/services/STOK"}, []string{"STOK"}},
		{"ntfy", chCfg{"server_url": "https://alice:NPASS@ntfy.home", "topic": "NTOPIC", "token": "NBEARER"}, []string{"NPASS", "NTOPIC", "alice"}},
		{"gotify", chCfg{"server_url": "https://bob:GPASS@gotify.home", "app_token": "GTOK"}, []string{"GPASS", "GTOK", "bob"}},
		{"webhook", chCfg{"url": "https://riot:WPASS@n8n.home/webhook/WTOK", "headers": map[string]interface{}{"Authorization": "Bearer WHDR"}}, []string{"WPASS", "WTOK"}},
	}
	for _, row := range rows {
		row := row
		t.Run(row.typ, func(t *testing.T) {
			s := newSecretsHarness(t)
			id := s.seed(row.typ, row.config)
			s.h.dispatcher.SetHTTPClient(&http.Client{Transport: failingTransport{}})

			rec, body := s.do("POST", "/api/v1/settings/notification-channels/"+strconv.FormatInt(id, 10)+"/test", nil)

			require.Equal(t, http.StatusOK, rec.Code, body)
			var resp struct {
				Success bool   `json:"success"`
				Error   string `json:"error"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), body)
			assert.False(t, resp.Success)
			for _, leak := range row.mustNotLeak {
				assert.NotContains(t, resp.Error, leak, "%s: %q must not appear in the test-send error", row.typ, leak)
			}
			assert.Contains(t, resp.Error, sentinel, "%s: the error must show the sentinel in place", row.typ)
			assert.Contains(t, resp.Error, "connection refused", "%s: the diagnostic must survive", row.typ)
		})
	}
}

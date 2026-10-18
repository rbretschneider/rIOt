package notify

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cfg = map[string]interface{}

func channel(typ string, c cfg) models.NotificationChannel {
	return models.NotificationChannel{ID: 7, Name: "n", Type: typ, Enabled: true, Config: c}
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// --- registry / sentinel ---

func TestHARDEN002_Pure_SentinelIsNotAPlausibleCredentialOrURL(t *testing.T) {
	assert.Equal(t, "[rIOt-redacted]", RedactedSecret, "the sentinel literal is part of the API contract")
	_, _, _, ok := splitURLUserinfo(RedactedSecret)
	assert.False(t, ok, "V-001: the sentinel must not look like an absolute URL")
}

func TestHARDEN002_AC018_Pure_SecretKeysRegistryMatchesTable(t *testing.T) {
	want := map[string][]string{
		"smtp":     {"password"},
		"ntfy":     {"token", "topic"},
		"telegram": {"bot_token"},
		"discord":  {"webhook_url"},
		"slack":    {"webhook_url"},
		"pushover": {"api_token", "user_key"},
		"gotify":   {"app_token"},
		"webhook":  {"url"},
	}
	for typ, keys := range want {
		got, known := SecretKeys(typ)
		assert.True(t, known, "%s must be a known type", typ)
		assert.ElementsMatch(t, keys, got, "secret keys for %s", typ)
	}
	got, known := SecretKeys("futurething")
	assert.False(t, known, "an unregistered type is unknown")
	assert.Nil(t, got, "unknown type returns no keys: every key is secret")

	// Mutating the returned slice must not corrupt the registry.
	ks, _ := SecretKeys("smtp")
	ks[0] = "tampered"
	again, _ := SecretKeys("smtp")
	assert.Equal(t, []string{"password"}, again, "SecretKeys must return a copy")
}

// --- redaction ---

func TestHARDEN002_AC020_Pure_RedactEveryRegistryType(t *testing.T) {
	type tc struct {
		typ     string
		config  cfg
		secrets map[string]string // key -> canary
		plain   cfg
	}
	cases := []tc{
		{"smtp", cfg{"password": "CANARY-smtp-pw", "host": "mail.example", "port": float64(587), "username": "alerts", "from": "a@x", "to": "b@x", "starttls": true},
			map[string]string{"password": "CANARY-smtp-pw"}, cfg{"host": "mail.example", "port": float64(587), "username": "alerts", "from": "a@x", "to": "b@x", "starttls": true}},
		{"ntfy", cfg{"token": "CANARY-ntfy-token", "topic": "CANARY-ntfy-topic", "server_url": "https://ntfy.example", "priority": "high", "priority_map": map[string]interface{}{"info": "low"}},
			map[string]string{"token": "CANARY-ntfy-token", "topic": "CANARY-ntfy-topic"}, cfg{"server_url": "https://ntfy.example", "priority": "high", "priority_map": map[string]interface{}{"info": "low"}}},
		{"telegram", cfg{"bot_token": "CANARY-tg:AAA", "chat_id": "42"},
			map[string]string{"bot_token": "CANARY-tg:AAA"}, cfg{"chat_id": "42"}},
		{"discord", cfg{"webhook_url": "https://discord.example/api/webhooks/1/CANARY-dc"},
			map[string]string{"webhook_url": "https://discord.example/api/webhooks/1/CANARY-dc"}, cfg{}},
		{"slack", cfg{"webhook_url": "https://hooks.slack.example/services/CANARY-sl"},
			map[string]string{"webhook_url": "https://hooks.slack.example/services/CANARY-sl"}, cfg{}},
		{"pushover", cfg{"api_token": "CANARY-po-api", "user_key": "CANARY-po-user", "device": "phone"},
			map[string]string{"api_token": "CANARY-po-api", "user_key": "CANARY-po-user"}, cfg{"device": "phone"}},
		{"gotify", cfg{"app_token": "CANARY-gt", "server_url": "https://gotify.example"},
			map[string]string{"app_token": "CANARY-gt"}, cfg{"server_url": "https://gotify.example"}},
		{"webhook", cfg{"url": "https://hook.example/CANARY-wh", "headers": map[string]interface{}{"Authorization": "Bearer CANARY-hdr"}},
			map[string]string{"url": "https://hook.example/CANARY-wh"}, cfg{}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.typ, func(t *testing.T) {
			ch := channel(c.typ, c.config)
			out := RedactChannel(ch)

			for k := range c.secrets {
				assert.Equal(t, RedactedSecret, out.Config[k], "%s.%s must be the sentinel", c.typ, k)
			}
			for k, v := range c.plain {
				assert.Equal(t, v, out.Config[k], "%s.%s is non-secret and must be returned as stored", c.typ, k)
			}
			body := mustJSON(t, out)
			for _, canary := range c.secrets {
				assert.NotContains(t, body, canary, "%s: canary secret must not appear anywhere in the serialised channel", c.typ)
			}
			assert.NotContains(t, body, "CANARY-hdr", "%s: header value must not appear", c.typ)
			assert.Equal(t, ch.ID, out.ID)
			assert.Equal(t, ch.Name, out.Name)
			assert.Equal(t, ch.Type, out.Type)
			assert.Equal(t, ch.Enabled, out.Enabled)
		})
	}
}

func TestHARDEN002_AC021_Pure_WebhookURLAndHeaderValuesRedactedNamesKept(t *testing.T) {
	ch := channel("webhook", cfg{
		"url":     "https://hook.example/T0KEN",
		"headers": map[string]interface{}{"Authorization": "Bearer abc123", "X-Env": "prod", "X-Empty": ""},
	})
	out := RedactChannel(ch)

	assert.Equal(t, RedactedSecret, out.Config["url"])
	headers, ok := out.Config["headers"].(map[string]interface{})
	require.True(t, ok, "headers must stay a map")
	assert.Equal(t, map[string]interface{}{"Authorization": RedactedSecret, "X-Env": RedactedSecret, "X-Empty": ""}, headers,
		"every header name stays; set values become the sentinel; empty stays empty")
	body := mustJSON(t, out)
	assert.NotContains(t, body, "T0KEN")
	assert.NotContains(t, body, "abc123")
}

func TestHARDEN002_AC021_Pure_NonMapHeadersFailClosed(t *testing.T) {
	out := RedactChannel(channel("webhook", cfg{"url": "https://h/x", "headers": "Authorization: Bearer LEAK"}))
	assert.Equal(t, RedactedSecret, out.Config["headers"], "a headers value that is not a map must be replaced wholesale")
	assert.NotContains(t, mustJSON(t, out), "LEAK")

	out = RedactChannel(channel("webhook", cfg{"url": "https://h/x", "headers": nil}))
	assert.Nil(t, out.Config["headers"], "an absent/nil headers value stays nil")
}

func TestHARDEN002_AC022_Pure_UnsetSecretsNotSentinel(t *testing.T) {
	absent := RedactChannel(channel("ntfy", cfg{"server_url": "https://ntfy.sh"}))
	_, has := absent.Config["token"]
	assert.False(t, has, "an absent secret key must not be invented")

	empty := RedactChannel(channel("ntfy", cfg{"token": "", "topic": "t"}))
	assert.Equal(t, "", empty.Config["token"], "an empty secret stays empty, never the sentinel")
	assert.Equal(t, RedactedSecret, empty.Config["topic"])

	nilVal := RedactChannel(channel("ntfy", cfg{"token": nil}))
	assert.Equal(t, "", nilVal.Config["token"], "a nil secret reads as unset")
}

func TestHARDEN002_AC023_Pure_UnknownTypeFullyRedacted(t *testing.T) {
	out := RedactChannel(channel("futurething", cfg{"a": "x1", "b": "y2", "n": float64(5), "empty": "", "m": map[string]interface{}{"k": "v"}}))
	assert.Equal(t, RedactedSecret, out.Config["a"])
	assert.Equal(t, RedactedSecret, out.Config["b"])
	assert.Equal(t, RedactedSecret, out.Config["n"], "non-string values are secret too")
	assert.Equal(t, RedactedSecret, out.Config["m"], "nested maps are replaced wholesale")
	assert.Equal(t, "", out.Config["empty"], "an empty value stays empty so the UI can tell it is unset")
	body := mustJSON(t, out)
	assert.NotContains(t, body, "x1")
	assert.NotContains(t, body, "y2")
}

func TestHARDEN002_Pure_RedactNilConfigAndNoMutation(t *testing.T) {
	out := RedactChannel(channel("smtp", nil))
	assert.NotNil(t, out.Config, "a nil config becomes an empty map")

	in := cfg{"password": "P1", "nested": map[string]interface{}{"x": "y"}, "server_url": "https://u:p@h"}
	ch := channel("smtp", in)
	out = RedactChannel(ch)
	assert.Equal(t, "P1", in["password"], "the input must not be mutated")
	assert.Equal(t, "https://u:p@h", in["server_url"], "the input must not be mutated")

	out.Config["nested"].(map[string]interface{})["x"] = "changed"
	assert.Equal(t, "y", in["nested"].(map[string]interface{})["x"], "the output must not alias the input's nested maps")
}

func TestHARDEN002_AC019_C1_Pure_URLUserinfoRedacted(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"user and password", "https://alice:S3cret@ntfy.home", "https://[rIOt-redacted]@ntfy.home"},
		{"with path", "https://alice:S3cret@ntfy.home/x/y", "https://[rIOt-redacted]@ntfy.home/x/y"},
		{"with port and query", "http://bob:Pw2@gotify.home:8080/p?q=1#f", "http://[rIOt-redacted]@gotify.home:8080/p?q=1#f"},
		{"username only", "https://tokenuser@ntfy.home", "https://[rIOt-redacted]@ntfy.home"},
		{"no userinfo", "https://ntfy.home/x", "https://ntfy.home/x"},
		{"at sign only in path", "https://ntfy.home/u@x", "https://ntfy.home/u@x"},
		{"at sign only in query", "https://ntfy.home?e=a@b", "https://ntfy.home?e=a@b"},
		{"not a URL", "alerts@example.com", "alerts@example.com"},
		{"empty", "", ""},
		{"already redacted", "https://[rIOt-redacted]@ntfy.home", "https://[rIOt-redacted]@ntfy.home"},
		{"last at sign wins", "https://a:p@ss@h.example/x", "https://[rIOt-redacted]@h.example/x"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for _, typ := range []string{"ntfy", "gotify"} {
				out := RedactChannel(channel(typ, cfg{"server_url": tc.in}))
				assert.Equal(t, tc.want, out.Config["server_url"], "%s server_url %q", typ, tc.in)
			}
		})
	}

	out := RedactChannel(channel("ntfy", cfg{"server_url": "https://alice:S3cret@ntfy.home", "priority": "p@ss"}))
	assert.NotContains(t, mustJSON(t, out), "S3cret")
	assert.NotContains(t, mustJSON(t, out), "alice")
	assert.Equal(t, "p@ss", out.Config["priority"], "non-URL values are untouched")
}

func TestHARDEN002_C1_Pure_SplitURLUserinfo(t *testing.T) {
	cases := []struct {
		in                     string
		prefix, userinfo, rest string
		ok                     bool
	}{
		{"https://u:p@h/x", "https://", "u:p", "h/x", true},
		{"https://h/x", "https://", "", "h/x", true},
		{"https://[rIOt-redacted]@h", "https://", "[rIOt-redacted]", "h", true},
		{"HTTP://U@H", "HTTP://", "U", "H", true},
		{"a+b-c.d://u@h", "a+b-c.d://", "u", "h", true},
		{"https://h@x@y/p", "https://", "h@x", "y/p", true},
		{"https://h/p@q", "https://", "", "h/p@q", true},
		{"https://h?x=u@v", "https://", "", "h?x=u@v", true},
		{"https://h#u@v", "https://", "", "h#u@v", true},
		{"https://u@", "https://", "u", "", true},
		{"://u@h", "", "", "", false},
		{"1http://u@h", "", "", "", false},
		{"ht tp://u@h", "", "", "", false},
		{"h_t://u@h", "", "", "", false},
		{"no scheme u@h", "", "", "", false},
		{"", "", "", "", false},
	}
	for _, tc := range cases {
		p, ui, rest, ok := splitURLUserinfo(tc.in)
		assert.Equal(t, tc.ok, ok, "ok for %q", tc.in)
		assert.Equal(t, tc.prefix, p, "prefix for %q", tc.in)
		assert.Equal(t, tc.userinfo, ui, "userinfo for %q", tc.in)
		assert.Equal(t, tc.rest, rest, "rest for %q", tc.in)
	}
}

// --- merge ---

func TestHARDEN002_AC025_Pure_SentinelKeepsStoredSecret(t *testing.T) {
	stored := channel("smtp", cfg{"password": "P1", "host": "h"})
	got := MergeSecrets(&stored, channel("smtp", cfg{"password": RedactedSecret, "host": "h2"}))
	assert.Equal(t, "P1", got["password"], "sentinel keeps the stored secret")
	assert.Equal(t, "h2", got["host"])
}

func TestHARDEN002_AC026_Pure_OmittedKeyKeepsStoredSecret(t *testing.T) {
	stored := channel("discord", cfg{"webhook_url": "https://discord.example/W1"})
	got := MergeSecrets(&stored, channel("discord", cfg{}))
	assert.Equal(t, "https://discord.example/W1", got["webhook_url"], "an omitted secret key keeps the stored value")

	got = MergeSecrets(&stored, channel("discord", nil))
	assert.Equal(t, "https://discord.example/W1", got["webhook_url"], "a nil config also keeps it")
}

func TestHARDEN002_AC027_Pure_NewValueReplaces(t *testing.T) {
	stored := channel("gotify", cfg{"app_token": "OLD"})
	got := MergeSecrets(&stored, channel("gotify", cfg{"app_token": "NEW"}))
	assert.Equal(t, "NEW", got["app_token"])
}

func TestHARDEN002_AC028_Pure_EmptyStringClears(t *testing.T) {
	stored := channel("ntfy", cfg{"token": "tk", "topic": "t"})
	got := MergeSecrets(&stored, channel("ntfy", cfg{"token": "", "topic": RedactedSecret}))
	assert.Equal(t, "", got["token"], "an explicit empty string clears the secret")
	assert.Equal(t, "t", got["topic"], "the other secret is untouched")
}

func TestHARDEN002_AC029_Pure_SentinelNeverPersisted(t *testing.T) {
	t.Run("a: nothing stored", func(t *testing.T) {
		stored := channel("smtp", cfg{"host": "h"})
		got := MergeSecrets(&stored, channel("smtp", cfg{"password": RedactedSecret}))
		assert.Equal(t, "", got["password"])
	})
	t.Run("b: type change inherits nothing", func(t *testing.T) {
		stored := channel("smtp", cfg{"password": "P1"})
		got := MergeSecrets(&stored, channel("pushover", cfg{"api_token": RedactedSecret, "password": RedactedSecret}))
		assert.Equal(t, "", got["api_token"], "api_token must not inherit across a type change")
		assert.Equal(t, "", got["password"], "unknown-to-new-type key also resolves to empty")
	})
	t.Run("b2: discord url must not become a slack target", func(t *testing.T) {
		stored := channel("discord", cfg{"webhook_url": "https://discord.example/W"})
		got := MergeSecrets(&stored, channel("slack", cfg{"webhook_url": RedactedSecret}))
		assert.Equal(t, "", got["webhook_url"])
	})
	t.Run("c: create", func(t *testing.T) {
		got := MergeSecrets(nil, channel("telegram", cfg{"bot_token": RedactedSecret, "chat_id": "1"}))
		assert.Equal(t, "", got["bot_token"])
		assert.Equal(t, "1", got["chat_id"])
	})
	t.Run("d: sentinel in a non-secret key is swept", func(t *testing.T) {
		got := MergeSecrets(nil, channel("smtp", cfg{"host": RedactedSecret}))
		assert.Equal(t, "", got["host"], "V-005: no stored value may equal the sentinel")
	})
	t.Run("e: sentinel one level down is swept", func(t *testing.T) {
		got := MergeSecrets(nil, channel("ntfy", cfg{"priority_map": map[string]interface{}{"info": RedactedSecret, "warning": "high"}}))
		assert.Equal(t, map[string]interface{}{"info": "", "warning": "high"}, got["priority_map"])
	})
}

func TestHARDEN002_AC029_C1_Pure_UserinfoSentinelNeverPersisted(t *testing.T) {
	const sentinelURL = "https://[rIOt-redacted]@ntfy.home"

	t.Run("a: nothing stored to keep", func(t *testing.T) {
		stored := channel("ntfy", cfg{"server_url": "https://ntfy.home"})
		got := MergeSecrets(&stored, channel("ntfy", cfg{"server_url": sentinelURL}))
		assert.Equal(t, "https://ntfy.home", got["server_url"], "the userinfo is stripped, the host kept")
	})
	t.Run("b: create", func(t *testing.T) {
		got := MergeSecrets(nil, channel("ntfy", cfg{"server_url": sentinelURL + "/x"}))
		assert.Equal(t, "https://ntfy.home/x", got["server_url"])
	})
	t.Run("c: type change", func(t *testing.T) {
		stored := channel("gotify", cfg{"server_url": "https://bob:Pw2@gotify.home"})
		got := MergeSecrets(&stored, channel("ntfy", cfg{"server_url": "https://[rIOt-redacted]@gotify.home"}))
		assert.Equal(t, "https://gotify.home", got["server_url"], "a type change inherits no userinfo")
	})
	t.Run("d: stored value is not a string", func(t *testing.T) {
		stored := channel("ntfy", cfg{"server_url": float64(1)})
		got := MergeSecrets(&stored, channel("ntfy", cfg{"server_url": sentinelURL}))
		assert.Equal(t, "https://ntfy.home", got["server_url"])
	})
	t.Run("e: stored userinfo itself is the sentinel (corrupt data)", func(t *testing.T) {
		stored := channel("ntfy", cfg{"server_url": sentinelURL})
		got := MergeSecrets(&stored, channel("ntfy", cfg{"server_url": sentinelURL}))
		assert.Equal(t, "https://ntfy.home", got["server_url"], "a stored sentinel userinfo must not be propagated")
	})
}

func TestHARDEN002_AC025_C1_Pure_NameOnlyPutPreservesUserinfo(t *testing.T) {
	stored := channel("ntfy", cfg{"server_url": "https://alice:S3cret@ntfy.home", "topic": "t"})
	listed := RedactChannel(stored)
	require.Equal(t, "https://[rIOt-redacted]@ntfy.home", listed.Config["server_url"])

	got := MergeSecrets(&stored, listed)
	assert.Equal(t, "https://alice:S3cret@ntfy.home", got["server_url"], "round-tripping the listed form must keep the stored credential")
	assert.Equal(t, "t", got["topic"])

	t.Run("a new credential replaces the old one", func(t *testing.T) {
		sub := channel("ntfy", cfg{"server_url": "https://carol:N3w@ntfy.home"})
		assert.Equal(t, "https://carol:N3w@ntfy.home", MergeSecrets(&stored, sub)["server_url"])
	})
	t.Run("removing the userinfo removes the credential", func(t *testing.T) {
		sub := channel("ntfy", cfg{"server_url": "https://ntfy.home"})
		assert.Equal(t, "https://ntfy.home", MergeSecrets(&stored, sub)["server_url"])
	})
	t.Run("editing the host keeps the stored userinfo (residual F-7)", func(t *testing.T) {
		sub := channel("ntfy", cfg{"server_url": "https://[rIOt-redacted]@other.home"})
		assert.Equal(t, "https://alice:S3cret@other.home", MergeSecrets(&stored, sub)["server_url"])
	})
}

func TestHARDEN002_AC030_Pure_WebhookHeaderMerge(t *testing.T) {
	stored := channel("webhook", cfg{"url": "https://h/x", "headers": map[string]interface{}{"Authorization": "Bearer A", "X-Old": "o"}})

	t.Run("a: omitted keeps stored headers", func(t *testing.T) {
		got := MergeSecrets(&stored, channel("webhook", cfg{"url": RedactedSecret}))
		assert.Equal(t, map[string]interface{}{"Authorization": "Bearer A", "X-Old": "o"}, got["headers"])
		assert.Equal(t, "https://h/x", got["url"])
	})
	t.Run("a2: nil keeps stored headers", func(t *testing.T) {
		got := MergeSecrets(&stored, channel("webhook", cfg{"headers": nil}))
		assert.Equal(t, map[string]interface{}{"Authorization": "Bearer A", "X-Old": "o"}, got["headers"])
	})
	t.Run("b: present is the complete set", func(t *testing.T) {
		got := MergeSecrets(&stored, channel("webhook", cfg{"headers": map[string]interface{}{"Authorization": RedactedSecret, "X-New": "n"}}))
		assert.Equal(t, map[string]interface{}{"Authorization": "Bearer A", "X-New": "n"}, got["headers"], "X-Old must be removed")
	})
	t.Run("c: sentinel for a name with no stored value is empty", func(t *testing.T) {
		got := MergeSecrets(&stored, channel("webhook", cfg{"headers": map[string]interface{}{"X-Brand-New": RedactedSecret}}))
		assert.Equal(t, map[string]interface{}{"X-Brand-New": ""}, got["headers"])
	})
	t.Run("d: empty map removes everything", func(t *testing.T) {
		got := MergeSecrets(&stored, channel("webhook", cfg{"headers": map[string]interface{}{}}))
		assert.Equal(t, map[string]interface{}{}, got["headers"])
	})
	t.Run("e: type change inherits no headers", func(t *testing.T) {
		other := channel("smtp", cfg{"password": "P"})
		got := MergeSecrets(&other, channel("webhook", cfg{"headers": map[string]interface{}{"Authorization": RedactedSecret}}))
		assert.Equal(t, map[string]interface{}{"Authorization": ""}, got["headers"])
		got = MergeSecrets(&other, channel("webhook", cfg{}))
		_, has := got["headers"]
		assert.False(t, has, "nothing to inherit")
	})
	t.Run("f: create with sentinel header value", func(t *testing.T) {
		got := MergeSecrets(nil, channel("webhook", cfg{"headers": map[string]interface{}{"Authorization": RedactedSecret}}))
		assert.Equal(t, map[string]interface{}{"Authorization": ""}, got["headers"])
	})
	t.Run("g: a non-map headers value is stored as submitted", func(t *testing.T) {
		got := MergeSecrets(&stored, channel("webhook", cfg{"headers": "oops"}))
		assert.Equal(t, "oops", got["headers"])
	})
}

func TestHARDEN002_AC031_Pure_NonSecretKeysReplaced(t *testing.T) {
	stored := channel("smtp", cfg{"host": "h1", "from": "a@x", "password": "P"})
	got := MergeSecrets(&stored, channel("smtp", cfg{"host": "h2", "password": RedactedSecret}))
	assert.Equal(t, cfg{"host": "h2", "password": "P"}, got, "from must be gone, password kept")
}

func TestHARDEN002_Pure_MergeUnknownTypeUsesUnionOfKeys(t *testing.T) {
	stored := channel("futurething", cfg{"a": "x1", "b": "y2"})

	got := MergeSecrets(&stored, channel("futurething", cfg{"a": RedactedSecret}))
	assert.Equal(t, cfg{"a": "x1", "b": "y2"}, got, "b was omitted so it is kept; a is kept via the sentinel")

	got = MergeSecrets(&stored, channel("futurething", cfg{"a": "", "b": "new"}))
	assert.Equal(t, cfg{"a": "", "b": "new"}, got, "an unknown-type key can be dropped only by sending empty")
}

func TestHARDEN002_Pure_MergeNeverMutatesInputs(t *testing.T) {
	stored := channel("webhook", cfg{"url": "https://h/x", "headers": map[string]interface{}{"A": "1"}})
	sub := channel("webhook", cfg{"url": RedactedSecret, "headers": map[string]interface{}{"A": RedactedSecret}})
	got := MergeSecrets(&stored, sub)

	assert.Equal(t, RedactedSecret, sub.Config["url"], "submitted must not be mutated")
	assert.Equal(t, RedactedSecret, sub.Config["headers"].(map[string]interface{})["A"], "submitted nested map must not be mutated")
	got["headers"].(map[string]interface{})["A"] = "changed"
	assert.Equal(t, "1", stored.Config["headers"].(map[string]interface{})["A"], "the result must not alias the stored headers")
}

// --- scrubbing ---

func TestHARDEN002_C2_Pure_ScrubSecretsMatchesStdlibForms(t *testing.T) {
	// Produce the real *url.Error text for a URL with userinfo and a path token.
	const secretURL = "https://riot:WPASS@n8n.home/webhook/WTOK"
	rt := rtFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("dial tcp: connection refused") })
	_, err := (&http.Client{Transport: rt}).Post(secretURL, "application/json", strings.NewReader("{}"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "riot:***@", "precondition: net/http renders the password as ***")
	require.Contains(t, err.Error(), "WTOK", "precondition: the path token is in the error text")

	ch := channel("webhook", cfg{"url": secretURL})
	got := ScrubSecrets("webhook: send: "+err.Error(), ch)
	assert.NotContains(t, got, "WTOK", "path token must be scrubbed from the stripPassword form")
	assert.NotContains(t, got, "WPASS")
	assert.NotContains(t, got, "n8n.home/webhook", "the whole URL form is replaced")
	assert.Contains(t, got, RedactedSecret)
	assert.Contains(t, got, "connection refused", "the diagnostic part of the error survives")

	t.Run("u.Redacted form", func(t *testing.T) {
		text := "failed https://riot:xxxxx@n8n.home/webhook/WTOK done"
		got := ScrubSecrets(text, ch)
		assert.NotContains(t, got, "WTOK")
	})

	t.Run("strconv.Quote interior of a secret with quote and backslash", func(t *testing.T) {
		secret := `pa"ss\word`
		ch := channel("smtp", cfg{"password": secret})
		text := `smtp: login "` + `pa\"ss\\word` + `" rejected`
		got := ScrubSecrets(text, ch)
		assert.NotContains(t, got, `pa\"ss`, "the %q-escaped form must be scrubbed")
		assert.NotContains(t, got, `ss\\word`)
		assert.Equal(t, `smtp: login "`+RedactedSecret+`" rejected`, got)

		raw := ScrubSecrets("raw "+secret+" form", ch)
		assert.Equal(t, "raw "+RedactedSecret+" form", raw)
	})

	t.Run("path-escaped and query-escaped forms", func(t *testing.T) {
		ch := channel("telegram", cfg{"bot_token": "12 3:A/B?C"})
		for _, form := range []string{"12 3:A/B?C", "12%203:A%2FB%3FC", "12+3%3AA%2FB%3FC"} {
			got := ScrubSecrets("x "+form+" y", ch)
			assert.NotContains(t, got, form, "form %q must be scrubbed", form)
		}
	})
}

func TestHARDEN002_C2_Pure_ScrubUserinfoOfNonSecretURL(t *testing.T) {
	ch := channel("ntfy", cfg{"server_url": "https://alice:NPASS@ntfy.home", "topic": "NTOPIC", "token": "NBEARER"})
	text := `ntfy: send: Post "https://alice:***@ntfy.home/NTOPIC": dial tcp: refused`
	got := ScrubSecrets(text, ch)
	for _, leak := range []string{"alice", "NPASS", "NTOPIC", "NBEARER"} {
		assert.NotContains(t, got, leak)
	}
	assert.Contains(t, got, "ntfy.home", "the host is not a secret and stays in the error")
}

func TestHARDEN002_C2_Pure_ScrubWebhookHeaderValuesAndUnknownType(t *testing.T) {
	ch := channel("webhook", cfg{"url": "https://h/x", "headers": map[string]interface{}{"Authorization": "Bearer WHDR"}})
	assert.NotContains(t, ScrubSecrets("bad header Bearer WHDR rejected", ch), "WHDR")

	unknown := channel("futurething", cfg{"a": "x1", "nested": map[string]interface{}{"b": "deep-secret"}})
	got := ScrubSecrets("a=x1 b=deep-secret", unknown)
	assert.NotContains(t, got, "x1")
	assert.NotContains(t, got, "deep-secret")
}

func TestHARDEN002_C2_Pure_ScrubDoesNotCorruptOnShortOrOverlappingSecrets(t *testing.T) {
	// "redacted" appears inside the sentinel; a single-pass replace must not
	// rescan replacement text.
	ch := channel("smtp", cfg{"password": "redacted"})
	got := ScrubSecrets("password redacted here", ch)
	assert.Equal(t, "password "+RedactedSecret+" here", got)

	// Overlap: the longer variant wins.
	ch = channel("ntfy", cfg{"topic": "alerts", "token": "alerts-token"})
	got = ScrubSecrets("tok=alerts-token topic=alerts", ch)
	assert.Equal(t, "tok="+RedactedSecret+" topic="+RedactedSecret, got)
}

func TestHARDEN002_C2_Pure_ScrubNoSecretsLeavesTextAlone(t *testing.T) {
	ch := channel("smtp", cfg{"host": "mail.example", "password": ""})
	assert.Equal(t, "dial tcp mail.example:587: refused", ScrubSecrets("dial tcp mail.example:587: refused", ch))
	assert.Equal(t, "", ScrubSecrets("", ch))
}

func TestHARDEN002_AC034_Pure_ScrubErrorBehaviour(t *testing.T) {
	ch := channel("telegram", cfg{"bot_token": "999:SECRETTOKEN", "chat_id": "1"})

	assert.NoError(t, scrubError(nil, ch), "nil stays nil")

	orig := errors.New("telegram: send: Post \"https://api.telegram.org/bot999:SECRETTOKEN/sendMessage\": boom")
	scrubbed := scrubError(orig, ch)
	require.Error(t, scrubbed)
	assert.NotContains(t, scrubbed.Error(), "SECRETTOKEN")
	assert.Contains(t, scrubbed.Error(), RedactedSecret)
	assert.Nil(t, errors.Unwrap(scrubbed), "the scrubbed error must not wrap the original")
	assert.False(t, errors.Is(scrubbed, orig), "the original, unscrubbed error must not be recoverable")
}

// rtFunc is an http.RoundTripper for tests.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

package notify

// Added by QA Engineer (HARDEN-002). Adversarial probes of the C2 error-scrubbing
// choke point, driven through a real http.Client so the error text is what
// net/http actually produces, never a hand-built string.

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// qaDispatcher returns a dispatcher whose HTTP client uses rt.
func qaDispatcher(rt http.RoundTripper) *Dispatcher {
	d := NewDispatcher(testutil.NewMockNotifyRepo())
	d.SetHTTPClient(&http.Client{Transport: rt})
	return d
}

// Covers AC-034, AC-035 (C2): userinfo whose user and password contain
// percent-encoded '@', '"' and '\' must not survive in any serialization the
// standard library emits (stripPassword form, %q form).
func TestHARDEN002_QA_C2_SpecialCharUserinfoScrubbed(t *testing.T) {
	ch := models.NotificationChannel{ID: 1, Type: "webhook", Enabled: true, Config: cfg{
		"url": "https://al%40ice:p%40ss%22w%5Cd@n8n.home/webhook/WTOKQA",
	}}
	d := qaDispatcher(failingClient().Transport)

	err := d.TestChannel(context.Background(), ch)

	require.Error(t, err)
	for _, leak := range []string{"WTOKQA", "al@ice", "al%40ice", "p@ss", "p%40ss", `w"d`, `w\"d`, `w\\d`, "w%22w", "%5Cd"} {
		assert.NotContains(t, err.Error(), leak, "special-character credential %q leaked: %s", leak, err.Error())
	}
	assert.Contains(t, err.Error(), RedactedSecret)
}

// Covers AC-034, AC-035 (C2): an ntfy topic with spaces, quotes and non-ASCII
// is path-escaped by net/url and %q-quoted in the error. Neither form may leak.
func TestHARDEN002_QA_C2_AwkwardNtfyTopicScrubbed(t *testing.T) {
	for _, topic := range []string{`my topic`, `my "quoted" topic`, `tøpic-é-日本`, `a\b`} {
		topic := topic
		t.Run(topic, func(t *testing.T) {
			ch := models.NotificationChannel{ID: 1, Type: "ntfy", Enabled: true, Config: cfg{
				"server_url": "https://ntfy.home", "topic": topic,
			}}
			d := qaDispatcher(failingClient().Transport)

			err := d.TestChannel(context.Background(), ch)

			require.Error(t, err)
			assert.NotContains(t, err.Error(), topic, "raw topic leaked")
			// The path-escaped form is what the URL in *url.Error carries.
			assert.NotContains(t, err.Error(), "/"+strings.ReplaceAll(topic, " ", "%20"), "escaped topic leaked: %s", err.Error())
			assert.Contains(t, err.Error(), RedactedSecret)
		})
	}
}

// Covers AC-034 (C2): a request that net/http refuses to build (control byte in
// the topic) produces a "parse ..." error that embeds the whole raw URL,
// including the userinfo credential.
func TestHARDEN002_QA_C2_CreateRequestParseErrorScrubbed(t *testing.T) {
	ch := models.NotificationChannel{ID: 1, Type: "ntfy", Enabled: true, Config: cfg{
		"server_url": "https://alice:PARSEPASS@ntfy.home", "topic": "bad\x7ftopic-PTOPIC",
	}}
	d := qaDispatcher(failingClient().Transport)

	err := d.TestChannel(context.Background(), ch)

	require.Error(t, err)
	for _, leak := range []string{"PARSEPASS", "PTOPIC", "alice"} {
		assert.NotContains(t, err.Error(), leak, "leaked in: %s", err.Error())
	}
}

// Covers AC-034, AC-035 (C2): a remote service that echoes the credential back
// in an error response body (telegram and discord include the body in the error
// text) is scrubbed too, because scrubbing runs on the final text.
func TestHARDEN002_QA_C2_RemoteBodyEchoScrubbed(t *testing.T) {
	echo := func(secret string) http.RoundTripper {
		return rtFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 401,
				Status:     "401 Unauthorized",
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(`{"error":"bad token ` + secret + `"}`)),
				Request:    r,
			}, nil
		})
	}
	rows := []struct {
		name   string
		ch     models.NotificationChannel
		secret string
	}{
		{"telegram", models.NotificationChannel{ID: 1, Type: "telegram", Enabled: true, Config: cfg{"bot_token": "999:ECHOTG", "chat_id": "1"}}, "999:ECHOTG"},
		{"discord", models.NotificationChannel{ID: 1, Type: "discord", Enabled: true, Config: cfg{"webhook_url": "https://discord.example/api/webhooks/1/ECHODC"}}, "https://discord.example/api/webhooks/1/ECHODC"},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			d := qaDispatcher(echo(row.secret))

			err := d.TestChannel(context.Background(), row.ch)

			require.Error(t, err)
			assert.NotContains(t, err.Error(), "ECHO"+map[string]string{"telegram": "TG", "discord": "DC"}[row.name], "echoed credential leaked: %s", err.Error())
			assert.Contains(t, err.Error(), "401", "the status diagnostic must survive")
		})
	}
}

// Covers AC-034 (C2): a credential carried in the query string of the webhook
// URL (not userinfo) is the whole secret `url` value and must be scrubbed.
func TestHARDEN002_QA_C2_QueryStringCredentialScrubbed(t *testing.T) {
	ch := models.NotificationChannel{ID: 1, Type: "webhook", Enabled: true, Config: cfg{
		"url": "https://hooks.example/in?key=QKEYQA&x=1#frag",
	}}
	d := qaDispatcher(failingClient().Transport)

	err := d.TestChannel(context.Background(), ch)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "QKEYQA", "leaked: %s", err.Error())
}

// Covers AC-035: the scrubbed error carries no Unwrap chain back to the
// original, so errors.Unwrap/As cannot recover the unscrubbed text.
func TestHARDEN002_QA_C2_ScrubbedErrorHasNoUnwrapChain(t *testing.T) {
	ch := models.NotificationChannel{ID: 1, Type: "telegram", Enabled: true, Config: cfg{"bot_token": "999:UNWRAPTG", "chat_id": "1"}}
	d := qaDispatcher(failingClient().Transport)

	err := d.TestChannel(context.Background(), ch)

	require.Error(t, err)
	type unwrapper interface{ Unwrap() error }
	_, can := err.(unwrapper)
	assert.False(t, can, "the scrubbed error must not wrap the original")
}

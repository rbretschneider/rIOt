package notify

import (
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// RedactedSecret is the write-only sentinel. API responses carry it in place
// of a stored secret, and a PUT that submits it keeps the stored value
// (HARDEN-002 AD-006). It is fixed, ASCII, not a plausible credential, and not
// an absolute URL. Its frontend twin is REDACTED_SECRET in
// web/src/utils/secrets.ts; a test pins the two together.
const RedactedSecret = "[rIOt-redacted]"

// webhookHeadersKey is the webhook config key whose map values are all secret.
const webhookHeadersKey = "headers"

// secretKeys is the single registry of dedicated secret config keys per
// channel type. Redaction, merge and error scrubbing all read it. A type that
// is not listed is treated as "every key is secret" (fail closed).
var secretKeys = map[string][]string{
	"smtp":     {"password"},
	"ntfy":     {"token", "topic"},
	"telegram": {"bot_token"},
	"discord":  {"webhook_url"},
	"slack":    {"webhook_url"},
	"pushover": {"api_token", "user_key"},
	"gotify":   {"app_token"},
	"webhook":  {"url"}, // plus every value in headers
}

// SecretKeys returns the dedicated secret config keys for a channel type. An
// unknown type returns (nil, false), which means every key is secret. For
// "webhook" the values of the headers map are secret as well.
func SecretKeys(channelType string) (keys []string, known bool) {
	ks, ok := secretKeys[channelType]
	if !ok {
		return nil, false
	}
	return append([]string(nil), ks...), true
}

// nonEmpty reports whether a stored config value counts as "set".
func nonEmpty(v interface{}) bool {
	if v == nil {
		return false
	}
	if s, ok := v.(string); ok {
		return s != ""
	}
	return true
}

// isSentinel reports whether v is exactly the redaction sentinel.
func isSentinel(v interface{}) bool {
	s, ok := v.(string)
	return ok && s == RedactedSecret
}

// sentinelOrEmpty is what a response shows for a secret value.
func sentinelOrEmpty(v interface{}) string {
	if nonEmpty(v) {
		return RedactedSecret
	}
	return ""
}

func deepCopyValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		return deepCopyConfig(t)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i := range t {
			out[i] = deepCopyValue(t[i])
		}
		return out
	default:
		return v
	}
}

// deepCopyConfig copies a config map, recursing into nested maps and slices.
// A nil map becomes an empty map. The mock repository shares maps between
// calls, so callers must never mutate a map they did not copy.
func deepCopyConfig(cfg map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(cfg))
	for k, v := range cfg {
		out[k] = deepCopyValue(v)
	}
	return out
}

// splitURLUserinfo splits a string of the form scheme://[userinfo@]host... into
// its parts. ok requires a valid scheme followed by "://". The authority is the
// text after "://" up to the first '/', '?' or '#'; userinfo is the authority
// text before its last '@' (empty when there is none); prefix is "scheme://";
// rest is everything after that '@' (or after the prefix when there is no '@').
//
// It deliberately does not call url.Parse: the redacted form
// https://[rIOt-redacted]@host is not a parseable URL (brackets), yet must
// still be recognised when a client sends it back.
func splitURLUserinfo(v string) (prefix, userinfo, rest string, ok bool) {
	idx := strings.Index(v, "://")
	if idx < 1 {
		return "", "", "", false
	}
	scheme := v[:idx]
	for i := 0; i < len(scheme); i++ {
		c := scheme[i]
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		switch {
		case isLetter:
		case i > 0 && ((c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.'):
		default:
			return "", "", "", false
		}
	}
	prefix = v[:idx+3]
	afterScheme := v[idx+3:]
	authEnd := strings.IndexAny(afterScheme, "/?#")
	if authEnd < 0 {
		authEnd = len(afterScheme)
	}
	at := strings.LastIndex(afterScheme[:authEnd], "@")
	if at < 0 {
		return prefix, "", afterScheme, true
	}
	return prefix, afterScheme[:at], afterScheme[at+1:], true
}

// redactURLUserinfo rewrites a non-empty URL userinfo to the sentinel. Values
// that are not URLs, or carry no userinfo, come back unchanged.
func redactURLUserinfo(v string) string {
	prefix, userinfo, rest, ok := splitURLUserinfo(v)
	if !ok || userinfo == "" {
		return v
	}
	return prefix + RedactedSecret + "@" + rest
}

// RedactChannel returns a copy of ch that is safe to serialise in an API
// response. It never mutates ch or aliases its maps.
//
//   - Each secret key (every key for an unknown type) becomes the sentinel when
//     set, or "" when unset, so a client can tell "configured" from "empty".
//   - Webhook header values are redacted and their names kept; a headers value
//     that is not a map is replaced wholesale.
//   - Credentials in the userinfo of any other top-level URL value
//     (https://user:pass@host) become https://[rIOt-redacted]@host.
func RedactChannel(ch models.NotificationChannel) models.NotificationChannel {
	out := ch
	cfg := deepCopyConfig(ch.Config)
	handled := map[string]bool{}

	keys, known := SecretKeys(ch.Type)
	if !known {
		for k, v := range cfg {
			cfg[k] = sentinelOrEmpty(v)
			handled[k] = true
		}
	} else {
		for _, k := range keys {
			if v, present := cfg[k]; present {
				cfg[k] = sentinelOrEmpty(v)
			}
			handled[k] = true
		}
		if ch.Type == "webhook" {
			handled[webhookHeadersKey] = true
			switch hv := cfg[webhookHeadersKey].(type) {
			case nil:
			case map[string]interface{}:
				for name, val := range hv {
					hv[name] = sentinelOrEmpty(val)
				}
			default:
				cfg[webhookHeadersKey] = RedactedSecret
			}
		}
	}

	for k, v := range cfg {
		if handled[k] {
			continue
		}
		if s, ok := v.(string); ok {
			cfg[k] = redactURLUserinfo(s)
		}
	}

	out.Config = cfg
	return out
}

// MergeSecrets resolves a submitted channel config against the stored channel
// and returns the config to persist. stored is nil on create. It never mutates
// its inputs and never returns a config containing the sentinel.
//
// Per secret key: omitted or the sentinel keeps the stored value, a new value
// replaces it, "" clears it. A type change inherits nothing (the stored config
// is treated as empty). Webhook headers: omitted keeps the stored headers; a
// submitted map is the complete desired set, with a sentinel value keeping the
// stored value for that name. A sentinel userinfo in any other URL value keeps
// the stored userinfo of the same key. Non-secret keys are fully replaced.
func MergeSecrets(stored *models.NotificationChannel, submitted models.NotificationChannel) map[string]interface{} {
	out := deepCopyConfig(submitted.Config)

	inherit := stored != nil && stored.Type == submitted.Type
	prev := map[string]interface{}{}
	if inherit {
		prev = stored.Config
		if prev == nil {
			prev = map[string]interface{}{}
		}
	}

	keys, known := SecretKeys(submitted.Type)
	secret := map[string]bool{}
	if known {
		for _, k := range keys {
			secret[k] = true
		}
	} else {
		for k := range prev {
			secret[k] = true
		}
		for k := range submitted.Config {
			secret[k] = true
		}
	}
	isWebhook := submitted.Type == "webhook"
	if isWebhook {
		delete(secret, webhookHeadersKey)
	}

	for k := range secret {
		sv, present := submitted.Config[k]
		switch {
		case !present:
			if nonEmpty(prev[k]) {
				out[k] = deepCopyValue(prev[k])
			}
		case isSentinel(sv):
			if nonEmpty(prev[k]) {
				out[k] = deepCopyValue(prev[k])
			} else {
				out[k] = ""
			}
		}
		// Any other submitted value is already in out (a new value or "").
	}

	if isWebhook {
		mergeWebhookHeaders(out, submitted.Config, prev, inherit)
	}

	// Userinfo merge for non-secret URL values.
	for k, sv := range submitted.Config {
		if secret[k] || (isWebhook && k == webhookHeadersKey) {
			continue
		}
		s, ok := sv.(string)
		if !ok {
			continue
		}
		p, ui, rest, split := splitURLUserinfo(s)
		if !split || ui != RedactedSecret {
			continue
		}
		out[k] = p + rest
		if !inherit {
			continue
		}
		if ps, isStr := prev[k].(string); isStr {
			if _, pui, _, pok := splitURLUserinfo(ps); pok && pui != "" && pui != RedactedSecret {
				out[k] = p + pui + "@" + rest
			}
		}
	}

	sweepSentinels(out, 0)
	return out
}

// mergeWebhookHeaders applies the headers rules to out.
func mergeWebhookHeaders(out, submitted, prev map[string]interface{}, inherit bool) {
	hv, present := submitted[webhookHeadersKey]
	// A bare sentinel in place of the headers map means "unchanged", the same
	// as omitting it; otherwise the sweep would wipe the stored headers (QA-2).
	if !present || hv == nil || isSentinel(hv) {
		if inherit {
			if ph, ok := prev[webhookHeadersKey]; ok {
				out[webhookHeadersKey] = deepCopyValue(ph)
			}
		}
		return
	}
	m, isMap := hv.(map[string]interface{})
	if !isMap {
		return // stored as submitted
	}
	prevHeaders, _ := prev[webhookHeadersKey].(map[string]interface{})
	merged := make(map[string]interface{}, len(m))
	for name, val := range m {
		if isSentinel(val) {
			if nonEmpty(prevHeaders[name]) {
				merged[name] = deepCopyValue(prevHeaders[name])
			} else {
				merged[name] = ""
			}
			continue
		}
		merged[name] = deepCopyValue(val)
	}
	out[webhookHeadersKey] = merged
}

// sweepSentinels is the final guarantee that the sentinel is never persisted.
// It covers every string at the top level and one level down in nested maps:
// a value equal to the sentinel becomes "", and a URL whose userinfo is the
// sentinel loses its userinfo. Arrays and deeper nesting are not swept; nothing
// reads them as secrets.
func sweepSentinels(m map[string]interface{}, depth int) {
	for k, v := range m {
		switch t := v.(type) {
		case string:
			m[k] = sweepString(t)
		case map[string]interface{}:
			if depth == 0 {
				sweepSentinels(t, 1)
			}
		}
	}
}

func sweepString(s string) string {
	if s == RedactedSecret {
		return ""
	}
	if p, ui, rest, ok := splitURLUserinfo(s); ok && ui == RedactedSecret {
		return p + rest
	}
	return s
}

// ScrubSecrets replaces every secret value of ch found in text with the
// sentinel. The secret set is the registry secrets (every string for an
// unknown type), every webhook header value, and the username, password and
// raw userinfo of any URL in the config. Each value is matched in the forms Go
// actually emits: raw, path- and query-escaped, the url.URL String and
// Redacted forms, net/http's stripPassword form (user:***@host), and the
// strconv.Quote interior that *url.Error uses (HARDEN-002 AD-008, C2).
func ScrubSecrets(text string, ch models.NotificationChannel) string {
	if text == "" {
		return text
	}
	values := secretValues(ch)

	seen := map[string]bool{}
	var variants []string
	add := func(w string) {
		if w != "" && w != RedactedSecret && !seen[w] {
			seen[w] = true
			variants = append(variants, w)
		}
	}
	for _, v := range values {
		for _, w := range scrubVariants(v) {
			add(w)
			q := strconv.Quote(w)
			if inner := q[1 : len(q)-1]; inner != w {
				add(inner)
			}
		}
	}
	if len(variants) == 0 {
		return text
	}

	sort.SliceStable(variants, func(i, j int) bool { return len(variants[i]) > len(variants[j]) })
	pairs := make([]string, 0, len(variants)*2)
	for _, w := range variants {
		pairs = append(pairs, w, RedactedSecret)
	}
	// A single pass: replaced text is never rescanned, so a short secret cannot
	// corrupt an earlier replacement. Longer variants come first and win ties.
	return strings.NewReplacer(pairs...).Replace(text)
}

// secretValues collects the raw strings that must not appear in error text.
func secretValues(ch models.NotificationChannel) []string {
	var out []string
	addString := func(v interface{}) {
		if s, ok := v.(string); ok && s != "" && s != RedactedSecret {
			out = append(out, s)
		}
	}
	var addAll func(v interface{})
	addAll = func(v interface{}) {
		switch t := v.(type) {
		case string:
			addString(t)
		case map[string]interface{}:
			for _, x := range t {
				addAll(x)
			}
		case []interface{}:
			for _, x := range t {
				addAll(x)
			}
		}
	}

	keys, known := SecretKeys(ch.Type)
	if !known {
		for _, v := range ch.Config {
			addAll(v)
		}
	} else {
		for _, k := range keys {
			addString(ch.Config[k])
		}
		if ch.Type == "webhook" {
			addAll(ch.Config[webhookHeadersKey])
		}
	}

	// Credentials in the userinfo of any top-level URL value.
	for _, v := range ch.Config {
		s, ok := v.(string)
		if !ok {
			continue
		}
		_, ui, _, split := splitURLUserinfo(s)
		if !split || ui == "" || ui == RedactedSecret {
			continue
		}
		parts := []string{ui}
		if user, pass, found := strings.Cut(ui, ":"); found {
			parts = append(parts, user, pass)
		}
		for _, p := range parts {
			addString(p)
			if un, err := url.PathUnescape(p); err == nil && un != p {
				addString(un)
			}
		}
	}
	return out
}

// scrubVariants returns the textual forms in which v may appear in an error.
func scrubVariants(v string) []string {
	out := []string{v, url.PathEscape(v), url.QueryEscape(v)}
	if u, err := url.Parse(v); err == nil && u.Scheme != "" {
		out = append(out, u.String(), u.Redacted(), stripPassword(u))
	}
	return out
}

// stripPassword copies net/http's helper of the same name exactly: it is how
// the standard client renders a request URL inside *url.Error. Do not rebuild
// it with url.UserPassword, which would escape '*' as %2A.
func stripPassword(u *url.URL) string {
	_, passSet := u.User.Password()
	if passSet {
		return strings.Replace(u.String(), u.User.String()+"@", u.User.Username()+":***@", 1)
	}
	return u.String()
}

// scrubError returns nil for nil, otherwise a new error whose text has every
// secret of ch replaced. It deliberately has no Unwrap, so the original,
// unscrubbed error cannot be recovered with errors.Unwrap/As.
func scrubError(err error, ch models.NotificationChannel) error {
	if err == nil {
		return nil
	}
	return errors.New(ScrubSecrets(err.Error(), ch))
}

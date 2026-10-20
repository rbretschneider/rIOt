package redact

import (
	"net/url"
	"regexp"
	"strings"
)

var urlToken = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://(?:(?:<(?:masked|redacted)>)|[^\s"'<>])+`)
var pair = regexp.MustCompile(`(["']?)([a-zA-Z_][a-zA-Z0-9_./-]*)(["']?\s*[:=]\s*)("[^"\r\n]*"|'[^'\r\n]*'|[^\s,;}&]+)`)
var authorization = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(?:(?:bearer|basic)\s+)?[^\r\n,;]+`)
var jwt = regexp.MustCompile(`\beyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\b`)
var pem = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]+-----.*?(?:-----END [A-Z0-9 ]+-----|$)`)
var ansi = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\))`)

// URLCreds scans to the last @, including credentials containing slashes or @.
func URLCreds(s string) (string, int) { return urlCreds(s, "<redacted>") }

func urlCreds(s, marker string) (string, int) {
	count := 0
	out := urlToken.ReplaceAllStringFunc(s, func(token string) string {
		start := strings.Index(token, "://") + 3
		at := strings.LastIndex(token[start:], "@")
		if at >= 0 {
			at += start
			if colon := strings.Index(token[start:at], ":"); colon >= 0 {
				colon += start
				if token[colon+1:at] != marker {
					count++
				}
				token = token[:colon+1] + marker + token[at:]
			}
		}
		if q := strings.IndexByte(token, '?'); q >= 0 {
			end := len(token)
			if hash := strings.IndexByte(token[q:], '#'); hash >= 0 {
				end = q + hash
			}
			parts := strings.Split(token[q+1:end], "&")
			for i, p := range parts {
				k, v, found := strings.Cut(p, "=")
				decoded, err := url.QueryUnescape(k)
				if err == nil && found && SensitiveKey(decoded) && v != marker {
					parts[i] = k + "=" + marker
					count++
				}
			}
			token = token[:q+1] + strings.Join(parts, "&") + token[end:]
		}
		return token
	})
	return out, count
}

// MaskText is idempotent. Its count is the number of replacements, not secrets.
func MaskText(s, marker string) (string, int) {
	s, count := urlCreds(s, marker)
	s = authorization.ReplaceAllStringFunc(s, func(match string) string {
		if strings.Contains(match, marker) {
			return match
		}
		count++
		return authorization.FindStringSubmatch(match)[1] + marker
	})
	s = pair.ReplaceAllStringFunc(s, func(match string) string {
		p := pair.FindStringSubmatch(match)
		if !SensitiveKey(p[2]) {
			return match
		}
		value := p[4]
		quote := ""
		if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") {
			quote = value[:1]
			value = strings.Trim(value, quote)
		}
		if value == marker {
			return match
		}
		count++
		return p[1] + p[2] + p[3] + quote + marker + quote
	})
	for _, pattern := range []*regexp.Regexp{authorization, jwt, pem} {
		s = pattern.ReplaceAllStringFunc(s, func(match string) string {
			if strings.Contains(match, marker) {
				return match
			}
			count++
			if pattern == authorization {
				p := pattern.FindStringSubmatch(match)
				return p[1] + marker
			}
			return marker
		})
	}
	return s, count
}

func MaskLine(s string) (string, int) { return MaskText(s, "<masked>") }
func StripANSI(s string) string       { return ansi.ReplaceAllString(s, "") }

// Labels copies its input so cached Docker state is never modified by a reader.
func Labels(labels map[string]string) (map[string]string, int) {
	if labels == nil {
		return nil, 0
	}
	out := make(map[string]string, len(labels))
	total := 0
	for k, v := range labels {
		if strings.HasPrefix(k, "com.docker.compose.") || strings.HasPrefix(k, "riot.") {
			masked,n:=MaskText(v,"<redacted>")
			out[k] = masked
			total+=n
			continue
		}
		if SensitiveKey(k) {
			out[k] = "<redacted>"
			if v != "<redacted>" {
				total++
			}
			continue
		}
		masked, n := MaskText(v, "<redacted>")
		out[k] = masked
		total += n
	}
	return out, total
}

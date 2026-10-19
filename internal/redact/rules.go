// Package redact removes credentials before Docker data leaves an agent or is stored.
package redact

import (
	"regexp"
	"strings"
)

var sensitiveParts = []string{"PASSWORD", "PASSWD", "PASS", "PWD", "SECRET", "TOKEN", "APIKEY", "ACCESSKEY", "PRIVATE", "CREDENTIAL", "AUTH", "COOKIE", "SESSION", "DSN", "CONNECTIONSTRING", "SALT", "BEARER", "JWT", "WEBHOOK", "SIGNING", "SIGNATURE", "ENCRYPT", "HMAC", "PRIVKEY", "PASSPHRASE", "PSK", "LICENSE", "CONNSTR"}
var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
var acronymBoundary = regexp.MustCompile(`([A-Z])([A-Z][a-z])`)
var separators = regexp.MustCompile(`[_./\s-]+`)

// SensitiveKey intentionally accepts false positives rather than exposing secrets.
func SensitiveKey(key string) bool {
	key = camelBoundary.ReplaceAllString(key, "${1}_${2}")
	key = acronymBoundary.ReplaceAllString(key, "${1}_${2}")
	parts := separators.Split(strings.ToUpper(key), -1)
	joined := strings.Join(parts, "")
	for _, part := range sensitiveParts {
		if strings.Contains(joined, part) {
			return true
		}
	}
	for _, part := range parts {
		if strings.HasSuffix(part, "KEY") || part == "PW" || part == "OTP" || part == "SAS" || part == "SIG" {
			return true
		}
	}
	return false
}

// SafeEnv is the product-approved allowlist; every other environment value is hidden.
var SafeEnv = map[string]bool{
	"TZ": true, "PUID": true, "PGID": true, "UMASK": true,
	"UID": true, "GID": true, "LANG": true, "LANGUAGE": true, "LC_ALL": true,
	"PATH": true, "HOME": true, "HOSTNAME": true, "TERM": true,
	"NODE_ENV": true, "PYTHONUNBUFFERED": true,
}

func EnvPair(key, value string) (string, bool) {
	if SafeEnv[key] {
		masked,n:=MaskText(value,"<redacted>")
		return masked,n>0
	}
	return "<redacted>", value != "<redacted>"
}

package collectors

import "strings"

// allowedUnits is the set of _SYSTEMD_UNIT values whose entries are trusted
// as genuine auth-daemon output (AD-004). Journald derives _SYSTEMD_UNIT from
// the cgroup of the writer process via kernel-attested peer credentials;
// userspace cannot set it.
var allowedUnits = map[string]struct{}{
	"ssh.service":            {},
	"sshd.service":           {},
	"sudo.service":           {},
	"systemd-logind.service": {},
	"login.service":          {},
}

// allowedIdentifiers is the set of SYSLOG_IDENTIFIER values that are trusted
// as genuine auth-daemon output (AD-004). SYSLOG_IDENTIFIER is userspace-
// settable, but combined with _UID=0 (the primary gate) it provides defense-
// in-depth: a forgery would require a root-owned process intentionally
// identifying as a known auth daemon.
var allowedIdentifiers = map[string]struct{}{
	"sshd":  {},
	"sudo":  {},
	"login": {},
	"su":    {},
}

// authFailurePatterns is the fixed set of content substrings matched against
// the MESSAGE field. Applied only after the origin filter passes.
// Matching short-circuits on the first hit (AC-002, FR-003).
var authFailurePatterns = []string{
	"Failed password",
	"authentication failure",
	"Invalid user",
	"pam_unix(sudo:auth): authentication failure",
}

// matchesAuthFailure returns true when the journald JSON entry (as an
// already-unmarshalled map[string]interface{}) represents a genuine
// authentication failure from a trusted system daemon.
//
// Both an origin filter AND a content filter must pass (AD-004, SEC-001):
//
//  1. Origin filter: _UID must be "0" AND (_SYSTEMD_UNIT in allowedUnits OR
//     SYSLOG_IDENTIFIER in allowedIdentifiers).
//
//  2. Content filter: MESSAGE must contain at least one auth-failure substring.
//
// Entries that fail the origin filter are rejected without scanning MESSAGE,
// keeping the hot-path cost for non-auth journal traffic near zero.
func matchesAuthFailure(raw map[string]interface{}) bool {
	// --- Origin filter (SEC-001 defense-in-depth) ---

	// Gate 1: _UID must be "0" (kernel-attested; cannot be forged by userspace).
	uid, _ := raw["_UID"].(string)
	if uid != "0" {
		return false
	}

	// Gate 2: _SYSTEMD_UNIT OR SYSLOG_IDENTIFIER must be in the allow-list.
	unit, _ := raw["_SYSTEMD_UNIT"].(string)
	syslogID, _ := raw["SYSLOG_IDENTIFIER"].(string)

	_, unitOK := allowedUnits[unit]
	_, idOK := allowedIdentifiers[syslogID]
	if !unitOK && !idOK {
		return false
	}

	// --- Content filter ---

	message, _ := raw["MESSAGE"].(string)
	if message == "" {
		return false
	}

	for _, pattern := range authFailurePatterns {
		if strings.Contains(message, pattern) {
			return true
		}
	}

	return false
}

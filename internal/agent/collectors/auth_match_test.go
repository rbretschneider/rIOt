package collectors

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// buildEntry creates a minimal journald JSON map for testing.
func buildEntry(uid, unit, syslogID, message string) map[string]interface{} {
	m := map[string]interface{}{}
	if uid != "" {
		m["_UID"] = uid
	}
	if unit != "" {
		m["_SYSTEMD_UNIT"] = unit
	}
	if syslogID != "" {
		m["SYSLOG_IDENTIFIER"] = syslogID
	}
	if message != "" {
		m["MESSAGE"] = message
	}
	return m
}

// [AC-003, SEC-001] Forged logger entry with non-root UID is NOT counted.
func TestAC003_MatchesAuthFailure_SEC001_ForgedLoggerNonRootUID_NotCounted(t *testing.T) {
	entry := buildEntry("1000", "", "logger", "Failed password for root from 1.2.3.4 port 22 ssh2")
	assert.False(t, matchesAuthFailure(entry),
		"[SEC-001] forged logger entry with _UID=1000 must NOT match")
}

// [AC-002] Real sshd failure with _UID=0 and _SYSTEMD_UNIT=ssh.service is counted.
func TestAC002_MatchesAuthFailure_RealSSHDFailedPassword_Counted(t *testing.T) {
	entry := buildEntry("0", "ssh.service", "sshd", "Failed password for invalid user admin from 10.0.0.1 port 54321 ssh2")
	assert.True(t, matchesAuthFailure(entry),
		"[AC-002] real sshd Failed password entry must match")
}

// [AC-002] Real sshd failure using sshd.service unit name is counted.
func TestAC002_MatchesAuthFailure_RealSSHDService_Counted(t *testing.T) {
	entry := buildEntry("0", "sshd.service", "sshd", "Failed password for root from 192.168.1.100 port 22 ssh2")
	assert.True(t, matchesAuthFailure(entry),
		"[AC-002] sshd.service entry with Failed password must match")
}

// [AC-002] Real sudo PAM failure is counted.
func TestAC002_MatchesAuthFailure_RealSudoPAMFailure_Counted(t *testing.T) {
	entry := buildEntry("0", "sudo.service", "sudo", "pam_unix(sudo:auth): authentication failure; logname=alice uid=1000 euid=0 tty=/dev/pts/0 ruser=alice rhost=  user=alice")
	assert.True(t, matchesAuthFailure(entry),
		"[AC-002] sudo PAM authentication failure must match")
}

// [AC-002] Generic PAM authentication failure is counted.
func TestAC002_MatchesAuthFailure_GenericPAMAuthenticationFailure_Counted(t *testing.T) {
	entry := buildEntry("0", "systemd-logind.service", "login", "authentication failure; logname=LOGIN uid=0 euid=0 tty=/dev/tty1 ruser= rhost=  user=baduser")
	assert.True(t, matchesAuthFailure(entry),
		"[AC-002] PAM authentication failure from login must match")
}

// [AC-002] Invalid user entry from sshd is counted.
func TestAC002_MatchesAuthFailure_InvalidUser_Counted(t *testing.T) {
	entry := buildEntry("0", "ssh.service", "sshd", "Invalid user hacker from 203.0.113.42 port 12345")
	assert.True(t, matchesAuthFailure(entry),
		"[AC-002] Invalid user entry must match")
}

// [AC-003] Non-matching message content with trusted origin is NOT counted.
func TestAC003_MatchesAuthFailure_NonMatchingMessage_NotCounted(t *testing.T) {
	entry := buildEntry("0", "ssh.service", "sshd", "Accepted publickey for alice from 10.0.0.5 port 22 ssh2")
	assert.False(t, matchesAuthFailure(entry),
		"[AC-003] non-matching message must NOT match even with trusted origin")
}

// [AC-003] systemd.service with matching content but not in allow-list is NOT counted.
func TestAC003_MatchesAuthFailure_WrongUnit_NotCounted(t *testing.T) {
	entry := buildEntry("0", "cron.service", "cron", "authentication failure; logname= uid=0 euid=0 tty=cron ruser= rhost=  user=root")
	assert.False(t, matchesAuthFailure(entry),
		"[AC-003] cron.service with matching content but not in allow-list must NOT match")
}

// [AC-003, SEC-001] Entry with matching content, _UID=0 but NEITHER _SYSTEMD_UNIT NOR SYSLOG_IDENTIFIER is NOT counted.
func TestAC003_MatchesAuthFailure_MissingUnitAndIdentifier_NotCounted(t *testing.T) {
	entry := buildEntry("0", "", "", "Failed password for root from 1.2.3.4 port 22")
	assert.False(t, matchesAuthFailure(entry),
		"[AC-003] entry missing both _SYSTEMD_UNIT and SYSLOG_IDENTIFIER must NOT match")
}

// Empty MESSAGE is NOT counted.
func TestMatchesAuthFailure_EmptyMessage_NotCounted(t *testing.T) {
	entry := buildEntry("0", "ssh.service", "sshd", "")
	assert.False(t, matchesAuthFailure(entry),
		"empty MESSAGE must NOT match")
}

// Missing MESSAGE key is NOT counted.
func TestMatchesAuthFailure_MissingMessage_NotCounted(t *testing.T) {
	entry := map[string]interface{}{
		"_UID":              "0",
		"_SYSTEMD_UNIT":     "ssh.service",
		"SYSLOG_IDENTIFIER": "sshd",
	}
	assert.False(t, matchesAuthFailure(entry),
		"missing MESSAGE key must NOT match")
}

// [AC-002] All four content patterns produce a match when origin is trusted.
func TestAC002_MatchesAuthFailure_AllFourPatternsMatch(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{"Failed password", "Failed password for root from 1.2.3.4 port 22 ssh2"},
		{"authentication failure", "authentication failure; logname=LOGIN uid=0 euid=0 tty=/dev/tty1 ruser= rhost=  user=root"},
		{"Invalid user", "Invalid user badactor from 203.0.113.42 port 55123"},
		{"pam_unix sudo:auth", "pam_unix(sudo:auth): authentication failure; logname=alice uid=1000 euid=0 tty=/dev/pts/0 ruser=alice rhost=  user=alice"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := buildEntry("0", "ssh.service", "sshd", tt.message)
			assert.True(t, matchesAuthFailure(entry),
				"[AC-002] pattern %q must match", tt.name)
		})
	}
}

// [AC-002, SEC-001] SYSLOG_IDENTIFIER allow-list (without _SYSTEMD_UNIT) with _UID=0 is counted.
func TestAC002_MatchesAuthFailure_SyslogIdentifierAllowlist_Counted(t *testing.T) {
	// _SYSTEMD_UNIT absent; SYSLOG_IDENTIFIER=sshd; _UID=0 — must pass
	entry := buildEntry("0", "", "sshd", "Failed password for root from 1.2.3.4 port 22 ssh2")
	assert.True(t, matchesAuthFailure(entry),
		"[AC-002] trusted SYSLOG_IDENTIFIER with _UID=0 must match when _SYSTEMD_UNIT absent")
}

// [SEC-001] SYSLOG_IDENTIFIER=sshd with non-root UID is NOT counted (logger forgery path).
func TestSEC001_MatchesAuthFailure_SyslogIdentifierForge_NonRootUID_NotCounted(t *testing.T) {
	entry := buildEntry("501", "", "sshd", "Failed password for root from 1.2.3.4 port 22 ssh2")
	assert.False(t, matchesAuthFailure(entry),
		"[SEC-001] forged syslog identifier with non-root UID must NOT match")
}

// Table-driven test for all allow-listed _SYSTEMD_UNIT values.
func TestMatchesAuthFailure_AllowedUnits_AllMatch(t *testing.T) {
	units := []string{
		"ssh.service",
		"sshd.service",
		"sudo.service",
		"systemd-logind.service",
		"login.service",
	}
	for _, unit := range units {
		t.Run(unit, func(t *testing.T) {
			entry := buildEntry("0", unit, "", "Failed password for root from 1.2.3.4 port 22")
			assert.True(t, matchesAuthFailure(entry),
				"_SYSTEMD_UNIT=%q must be in the allow-list", unit)
		})
	}
}

// Table-driven test for all allow-listed SYSLOG_IDENTIFIER values.
func TestMatchesAuthFailure_AllowedIdentifiers_AllMatch(t *testing.T) {
	identifiers := []string{"sshd", "sudo", "login", "su"}
	for _, id := range identifiers {
		t.Run(id, func(t *testing.T) {
			entry := buildEntry("0", "", id, "Failed password for root from 1.2.3.4 port 22")
			assert.True(t, matchesAuthFailure(entry),
				"SYSLOG_IDENTIFIER=%q must be in the allow-list", id)
		})
	}
}

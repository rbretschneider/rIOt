package collectors

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeJSONLine builds a single journald JSON line with the given fields.
func makeJSONLine(uid, unit, syslogID, message string, ts time.Time) string {
	m := map[string]interface{}{
		"PRIORITY": "4",
		"MESSAGE":  message,
		"__REALTIME_TIMESTAMP": fmt.Sprintf("%d",
			ts.UnixNano()/int64(time.Microsecond)),
	}
	if uid != "" {
		m["_UID"] = uid
	}
	if unit != "" {
		m["_SYSTEMD_UNIT"] = unit
	}
	if syslogID != "" {
		m["SYSLOG_IDENTIFIER"] = syslogID
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// newTestLogsCollector returns a LogsCollector with a fresh counter for unit tests.
func newTestLogsCollector() (*LogsCollector, *authFailureCounter) {
	counter := &authFailureCounter{}
	lc := &LogsCollector{authCounter: counter}
	return lc, counter
}

// [AC-002] Four real-origin sshd/sudo lines each increment the counter.
func TestAC002_ParseAndCount_FourAuthPatterns_CounterIsFour(t *testing.T) {
	lc, counter := newTestLogsCollector()

	base := time.Now()
	lines := []string{
		makeJSONLine("0", "ssh.service", "sshd", "Failed password for root from 1.2.3.4 port 22 ssh2", base),
		makeJSONLine("0", "systemd-logind.service", "login", "authentication failure; logname=LOGIN uid=0 euid=0 tty=/dev/tty1 ruser= rhost=  user=baduser", base.Add(time.Second)),
		makeJSONLine("0", "ssh.service", "sshd", "Invalid user hacker from 203.0.113.42 port 12345", base.Add(2*time.Second)),
		makeJSONLine("0", "sudo.service", "sudo", "pam_unix(sudo:auth): authentication failure; logname=alice uid=1000 euid=0 tty=/dev/pts/0 ruser=alice rhost=  user=alice", base.Add(3*time.Second)),
	}

	input := []byte(joinLines(lines))
	entries, _ := lc.parseAndCount(input)

	require.Len(t, entries, 4, "[AC-002] all four lines must produce log entries")
	assert.Equal(t, 4, counter.Drain(),
		"[AC-002] counter must equal 4 — one increment per matching line")
}

// [AC-003, SEC-001] Forged logger entry with non-root UID does NOT increment the counter.
func TestAC003_ParseAndCount_ForgedLoggerEntry_NotCounted(t *testing.T) {
	lc, counter := newTestLogsCollector()

	line := makeJSONLine("1000", "", "logger", "Failed password for root from 1.2.3.4 port 22 ssh2", time.Now())
	lc.parseAndCount([]byte(line))

	assert.Equal(t, 0, counter.Drain(),
		"[SEC-001] forged logger entry must NOT increment the counter")
}

// [AC-003] Non-matching message content does NOT increment the counter.
func TestAC003_ParseAndCount_NonMatchingMessage_NotCounted(t *testing.T) {
	lc, counter := newTestLogsCollector()

	line := makeJSONLine("0", "ssh.service", "sshd", "Accepted publickey for alice from 10.0.0.5 port 22 ssh2", time.Now())
	lc.parseAndCount([]byte(line))

	assert.Equal(t, 0, counter.Drain(),
		"[AC-003] non-matching message must NOT increment the counter")
}

// [AC-007] Ten identical sshd-origin Failed password lines increment the counter to 10.
func TestAC007_ParseAndCount_TenIdenticalLines_CounterIsTen(t *testing.T) {
	lc, counter := newTestLogsCollector()

	base := time.Now()
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = makeJSONLine("0", "ssh.service", "sshd",
			"Failed password for root from 10.0.0.1 port 22 ssh2",
			base.Add(time.Duration(i)*time.Second))
	}

	lc.parseAndCount([]byte(joinLines(lines)))

	assert.Equal(t, 10, counter.Drain(),
		"[AC-007] 10 identical sshd-origin lines must produce counter = 10 (no de-duplication)")
}

// [AC-005] MarkReady is NOT called when parseAndCount runs (that is the caller's job).
// This verifies that counter stays not-ready after parseAndCount alone.
func TestAC005_ParseAndCount_DoesNotCallMarkReady(t *testing.T) {
	lc, counter := newTestLogsCollector()

	line := makeJSONLine("0", "ssh.service", "sshd", "Failed password for root from 1.2.3.4 port 22", time.Now())
	lc.parseAndCount([]byte(line))

	// parseAndCount itself does not call MarkReady — that is Collect's responsibility.
	assert.False(t, counter.IsReady(),
		"[AC-005] parseAndCount must not call MarkReady — Collect is responsible")
}

// [AC-006] When given an error (empty output), no entries are returned and counter stays 0.
func TestAC006_ParseAndCount_EmptyOutput_NoEntriesNoCount(t *testing.T) {
	lc, counter := newTestLogsCollector()

	entries, _ := lc.parseAndCount([]byte(""))
	assert.Empty(t, entries, "[AC-006] empty output must produce no log entries")
	assert.Equal(t, 0, counter.Drain(), "[AC-006] empty output must not increment counter")
}

// ParseAndCount updates lastSeen to the latest timestamp in the output.
func TestParseAndCount_UpdatesLastSeen(t *testing.T) {
	lc, _ := newTestLogsCollector()

	base := time.Now().Truncate(time.Second)
	later := base.Add(5 * time.Second)

	lines := []string{
		makeJSONLine("0", "sshd.service", "sshd", "some message", base),
		makeJSONLine("0", "sshd.service", "sshd", "another message", later),
	}

	lc.parseAndCount([]byte(joinLines(lines)))

	lc.mu.Lock()
	lastSeen := lc.lastSeen
	lc.mu.Unlock()

	// lastSeen should be later + 1µs
	assert.True(t, lastSeen.After(later),
		"lastSeen must be advanced beyond the latest timestamp in output")
}

// ParseAndCount handles malformed JSON lines gracefully.
func TestParseAndCount_MalformedJSON_Skipped(t *testing.T) {
	lc, counter := newTestLogsCollector()

	input := "{not valid json}\n" + makeJSONLine("0", "ssh.service", "sshd", "Failed password for root", time.Now())
	entries, _ := lc.parseAndCount([]byte(input))

	require.Len(t, entries, 1, "valid line after malformed one must still be parsed")
	assert.Equal(t, 1, counter.Drain(), "valid auth failure after malformed line must be counted")
}

// joinLines joins a slice of JSON strings with newlines.
func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		result += l
		if i < len(lines)-1 {
			result += "\n"
		}
	}
	return result
}

package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// DIETPI-PATH-001 FR-014: the server stores a refusal event only for a reason
// in the closed skip-reason set, and composes the message itself.

func refusalEventBody(typ, outcome, reason, message string) string {
	m := map[string]string{"type": typ, "command_id": euCmdID, "updater": "dietpi", "outcome": outcome, "reason": reason, "message": message, "severity": "info"}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestAgentEventExtra_DP_AC011_RefusalEventStored(t *testing.T) {
	f := newEventFixture(t)
	rec := f.post(t, "dev-1", refusalEventBody("extra_updater_finished", "skipped", "dpkg_locked", "FORGED <b>text</b>"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, f.events.Events, 1)
	e := f.events.Events[0]
	assert.Equal(t, models.EventExtraUpdaterFinished, e.Type)
	assert.Equal(t, models.SeverityWarning, e.Severity, "a refusal is a warning, whatever severity the agent states")
	assert.Equal(t, "DietPi update skipped on pivpn: the package lock was held (command "+euCmdID+", origin auto_patch)", e.Message)
	assert.NotContains(t, e.Message, "FORGED")
	assert.Equal(t, euCmdID, e.CommandID)

	t.Run("[AC-011] every known skip reason is accepted", func(t *testing.T) {
		for _, reason := range maintenance.SkipReasons() {
			f := newEventFixture(t)
			rec := f.post(t, "dev-1", refusalEventBody("extra_updater_finished", "skipped", reason, ""))
			assert.Equal(t, http.StatusOK, rec.Code, reason)
			require.Len(t, f.events.Events, 1, reason)
			assert.Equal(t, models.SeverityWarning, f.events.Events[0].Severity, reason)
		}
	})
	t.Run("[AC-011] the new reason has words", func(t *testing.T) {
		f := newEventFixture(t)
		f.post(t, "dev-1", refusalEventBody("extra_updater_finished", "skipped", "rundir_not_private", ""))
		require.Len(t, f.events.Events, 1)
		assert.Contains(t, f.events.Events[0].Message, "rIOt could not isolate /run/dietpi")
	})
	t.Run("[AC-011] a second end event for the command is a no-op, as before", func(t *testing.T) {
		f := newEventFixture(t)
		for i := 0; i < 3; i++ {
			rec := f.post(t, "dev-1", refusalEventBody("extra_updater_finished", "skipped", "dpkg_locked", ""))
			require.Equal(t, http.StatusOK, rec.Code)
		}
		assert.Len(t, f.events.Events, 1)
	})
}

func TestAgentEventExtra_DP_AC011_UnknownReasonDropped(t *testing.T) {
	bad := map[string]string{
		"an invented reason":      "because",
		"an empty reason":         "",
		"markup":                  "<script>alert(1)</script>",
		"a known reason in caps":  "DPKG_LOCKED",
		"a known reason and more": "dpkg_locked; rm -rf /",
		"a very long reason":      strings.Repeat("a", 5000),
		"a path":                  "/etc/passwd",
		"a reason with a newline": "dpkg_locked\nx",
	}
	for name, reason := range bad {
		reason := reason
		t.Run("[AC-011] "+name+" stores nothing", func(t *testing.T) {
			f := newEventFixture(t)
			rec := f.post(t, "dev-1", refusalEventBody("extra_updater_finished", "skipped", reason, "x"))
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Empty(t, f.events.Events)
		})
	}
	t.Run("[AC-011] a reason on a start event is rejected", func(t *testing.T) {
		f := newEventFixture(t)
		rec := f.post(t, "dev-1", refusalEventBody("extra_updater_started", "", "dpkg_locked", ""))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Empty(t, f.events.Events)
	})
	t.Run("[AC-011] a reason on a non-skipped end event is ignored and never stored", func(t *testing.T) {
		f := newEventFixture(t)
		rec := f.post(t, "dev-1", refusalEventBody("extra_updater_finished", "failed", "dpkg_locked", ""))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Len(t, f.events.Events, 1)
		assert.NotContains(t, f.events.Events[0].Message, "lock")
		assert.Contains(t, f.events.Events[0].Message, "DietPi update failed on pivpn")
	})
	t.Run("[AC-011] another device's command is still refused", func(t *testing.T) {
		f := newEventFixture(t)
		rec := f.post(t, "dev-2", refusalEventBody("extra_updater_finished", "skipped", "dpkg_locked", ""))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Empty(t, f.events.Events)
	})
}

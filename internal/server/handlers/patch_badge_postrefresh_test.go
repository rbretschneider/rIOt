package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// DIETPI-REFRESH-001 AC-023 (server half): a post-run refresh note on the apt
// result line, single-line or spanning lines, must not make the server read a
// run as less alarming (BR-006, DR-SEC-002). The server code is unchanged; this
// pins its behavior against the new summary shapes.

const (
	postRefreshNoteOneLine   = "post-run package list refresh failed: exit status 100"
	postRefreshNoteMultiLine = "post-run package list refresh failed: package process failed: context deadline exceeded\nsignal: terminated"
)

type classification struct {
	Kind    string
	Reasons []string
	Blocked []string
	Info    []string
	Parsed  refusalRun
}

func classify(status, msg string) classification {
	run := &models.Command{ID: "0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", Action: models.ActionOSUpdate, Status: status, ResultMsg: msg}
	in := healthyInput()
	in.Updates.PendingUpdates = 2
	in.Updates.ExtraUpdaters = pendingEnabledDietPi()
	in.LastRun = run
	b := computePatchBadge(in, badgeNow)
	return classification{Kind: b.Kind, Reasons: reasonCodes(b), Blocked: blockedCodes(b), Info: infoCodes(b), Parsed: parseRefusals(run)}
}

func TestPatchBadge_DR_AC023_PostRefreshNoteKeepsClassification(t *testing.T) {
	const detail = "held by another process"
	// The lock reason comes from the worker constant so that no string literal here
	// names a package-manager lock (the EX2 AC-029 scan).
	lockReason := maintenance.SkipDpkgLocked
	shapes := []struct {
		name        string
		refusalOnly bool
		build       func(note string) string
	}{
		{"(i) apt-failed run, updater skipped apt_not_successful", false, func(note string) string {
			line2 := "upgrade failed: E: Unable to correct problems"
			if note != "" {
				line2 += "; " + note
			}
			return "apt: failed; dietpi: skipped (apt_not_successful)\n" + line2 + "\ndietpi: skipped (apt_not_successful)"
		}},
		{"(ii) apt-succeeded run, updater refused with the lock reason", true, func(note string) string {
			line2 := maintenance.SuccessSummary
			if note != "" {
				line2 += "; " + note
			}
			return "apt: succeeded; dietpi: skipped (" + lockReason + ")\n" + line2 + "\ndietpi: skipped (" + lockReason + "); " + detail
		}},
	}
	notes := map[string]string{"single-line note": postRefreshNoteOneLine, "multi-line note": postRefreshNoteMultiLine}
	for _, shape := range shapes {
		for _, status := range []string{string(models.CommandFailed), string(models.CommandSucceeded)} {
			for noteName, note := range notes {
				t.Run("[AC-023] "+shape.name+" under status "+status+" with a "+noteName, func(t *testing.T) {
					without := classify(status, shape.build(""))
					with := classify(status, shape.build(note))
					// A multi-line note under a failed status of a refusal-only
					// run adds a line that is not an updater detail line. The
					// run is then read as failed, which is the more alarming
					// reading, never a quieter one. The worker never stores a
					// failed status for a refusal-only run; only a worker older
					// than DIETPI-PATH-001 did.
					if status == string(models.CommandFailed) && shape.refusalOnly && noteName == "multi-line note" {
						assert.Equal(t, badgeNeedsYou, with.Kind, "[AC-023] a multi-line note may only make the run more alarming")
						assert.Contains(t, with.Reasons, reasonLastRunFailed)
						return
					}
					assert.Equal(t, without.Kind, with.Kind, "[AC-023] badge kind changed by the note")
					assert.Equal(t, without.Reasons, with.Reasons, "[AC-023] needs-you reason codes changed by the note")
					assert.Equal(t, without.Blocked, with.Blocked, "[AC-023] blocked codes changed by the note")
					assert.Equal(t, without.Info, with.Info, "[AC-023] information codes changed by the note")
					assert.Equal(t, without.Parsed, with.Parsed, "[AC-023] refusals or their detail changed by the note")
				})
			}
		}
	}
}

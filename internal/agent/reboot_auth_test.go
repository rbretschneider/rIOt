package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

const (
	suffixStarted         = "Reboot authorized; reboot required after run: yes; reboot started: yes"
	suffixNotRequired     = "Reboot authorized; reboot required after run: no; reboot not started: not required"
	suffixAllowRebootOff  = "Reboot authorized; reboot required after run: yes; reboot not started: allow_reboot off"
	suffixRunNotSucessful = "reboot not started: run not successful"
)

// authHarness drives authorized os_update results through the real command
// lifecycle (intake, result queueing, post-result phase B) with a stubbed run.
type authHarness struct {
	*lifecycleHarness
	reboots          atomic.Int32
	queuedAtReboot   atomic.Bool
	rebootCommandID  string
	rebootRunnerErr  error
	required         atomic.Bool
	requiredPMs      []string
	requiredCheckHit atomic.Int32
}

func newAuthHarness(t *testing.T, allowReboot, required bool) *authHarness {
	t.Helper()
	h := &authHarness{lifecycleHarness: newLifecycleHarness(t), rebootCommandID: idA}
	h.required.Store(required)
	h.a.config.Commands.AllowReboot = allowReboot
	h.a.rebootDelay = time.Millisecond
	h.a.rebootReportWait = 40 * time.Millisecond
	h.a.rebootRequiredCheck = func(_ context.Context, pm string) bool {
		h.requiredCheckHit.Add(1)
		h.requiredPMs = append(h.requiredPMs, pm)
		return h.required.Load()
	}
	h.a.runner = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "sudo" && len(args) > 0 && args[0] == "systemctl" {
			h.queuedAtReboot.Store(h.a.out.Has(h.rebootCommandID))
			h.reboots.Add(1)
			return nil, h.rebootRunnerErr
		}
		return nil, nil
	}
	return h
}

// runOSUpdate submits an os_update whose run result is r and waits for the
// command to finish. authorized sets reboot_if_required on the payload.
func (h *authHarness) runOSUpdate(authorized bool, r commandExecResult) {
	h.t.Helper()
	h.a.executor = func(cr *cmdRun) commandExecResult {
		return h.a.attachRebootDecision(cr, authorized, r)
	}
	params := map[string]interface{}{"mode": "full"}
	if authorized {
		params[models.ParamRebootIfRequired] = true
	}
	h.submit(h.rebootCommandID, models.ActionOSUpdate, params)
	h.wait()
}

func successRun(message string) commandExecResult {
	ec := 0
	return commandExecResult{Status: models.ResultStatusSuccess, Message: message, ExitCode: &ec, PackageManager: pmAPT}
}

func failedRun(reason string) commandExecResult {
	ec := 100
	return commandExecResult{Status: models.ResultStatusError, Message: "run did not finish", ExitCode: &ec, PackageManager: pmAPT, Reason: reason}
}

func (h *authHarness) message() string { return h.resultFor(h.rebootCommandID).Message }

func (h *authHarness) telemetryPending() int { return len(h.a.telemetryNow) }

func (h *authHarness) agentEventMessages() []string {
	h.tr.mu.Lock()
	defer h.tr.mu.Unlock()
	var out []string
	for _, e := range h.tr.agentEvents {
		out = append(out, e.Message)
	}
	return out
}

func TestRebootAuth_PF_AC017_RequiredAfterSuccessRebootsOnceAfterResultQueued(t *testing.T) {
	h := newAuthHarness(t, true, true)

	h.runOSUpdate(true, successRun("Upgraded 9 packages"))

	require.Eventually(t, func() bool { return h.reboots.Load() == 1 }, 2*time.Second, time.Millisecond,
		"[AC-017] the host must be rebooted after the run")
	assert.True(t, h.queuedAtReboot.Load(), "[AC-017] the result must already be queued in the outbox when the reboot starts")
	assert.Equal(t, "Upgraded 9 packages | "+suffixStarted, h.message())
	time.Sleep(60 * time.Millisecond)
	assert.EqualValues(t, 1, h.reboots.Load(), "[AC-017] exactly one reboot per command")
}

func TestRebootAuth_PF_AC017_WaitsForDeliveryAck(t *testing.T) {
	h := newAuthHarness(t, true, true)
	h.a.rebootReportWait = 10 * time.Second

	h.runOSUpdate(true, successRun("ok"))

	time.Sleep(80 * time.Millisecond)
	assert.Zero(t, h.reboots.Load(), "[AC-017] no reboot while the server has not acknowledged the result")
	h.a.out.Ack(idA)
	require.Eventually(t, func() bool { return h.reboots.Load() == 1 }, 2*time.Second, time.Millisecond,
		"[AC-017] the reboot follows the acknowledgement")
	assert.False(t, h.queuedAtReboot.Load(), "[AC-017] the acknowledged result is gone from the outbox at reboot time")
}

func TestRebootAuth_PF_AC017_DeliveryWaitBoundedThenReboots(t *testing.T) {
	h := newAuthHarness(t, true, true)
	h.a.rebootReportWait = 30 * time.Millisecond

	h.runOSUpdate(true, successRun("ok"))

	require.Eventually(t, func() bool { return h.reboots.Load() == 1 }, 2*time.Second, time.Millisecond,
		"[AC-017] an unreachable server must not hold a required reboot forever")
	assert.True(t, h.queuedAtReboot.Load(), "[AC-017] the result stays durable in the outbox and is delivered after boot")
}

func TestRebootAuth_PF_AC018_NotRequiredNoReboot(t *testing.T) {
	h := newAuthHarness(t, true, false)

	h.runOSUpdate(true, successRun("Upgraded 2 packages"))

	assert.Equal(t, "Upgraded 2 packages | "+suffixNotRequired, h.message())
	time.Sleep(60 * time.Millisecond)
	assert.Zero(t, h.reboots.Load(), "[AC-018] ticking the box never means reboot unconditionally")
	assert.Equal(t, 1, h.telemetryPending(), "[AC-018] fresh update state is requested instead")
}

func TestRebootAuth_PF_AC019_FailedRunNoReboot(t *testing.T) {
	cases := []struct {
		name string
		run  commandExecResult
	}{
		{"failed", failedRun("")},
		{"timed out", failedRun(models.ReasonTimeout)},
		{"interrupted", failedRun(models.ReasonRestarted)},
	}
	for _, tc := range cases {
		t.Run("[AC-019] "+tc.name, func(t *testing.T) {
			h := newAuthHarness(t, true, true)

			h.runOSUpdate(true, tc.run)

			assert.Contains(t, h.message(), suffixRunNotSucessful, "[AC-019] the reason is 'run not successful'")
			assert.Contains(t, h.message(), "reboot required after run: yes", "the required fact is still reported truthfully")
			time.Sleep(60 * time.Millisecond)
			assert.Zero(t, h.reboots.Load(), "[AC-019] a run that did not succeed never reboots")
			assert.Equal(t, claimOK, h.a.claimPatchReboot(idA), "[AC-019] no reboot intent is consumed by a refused reboot")
		})
	}
}

func TestRebootAuth_PF_AC020_AgentVetoBeatsAuthorization(t *testing.T) {
	h := newAuthHarness(t, false, true)

	h.runOSUpdate(true, successRun("Upgraded 9 packages"))

	assert.Equal(t, "Upgraded 9 packages | "+suffixAllowRebootOff, h.message())
	time.Sleep(60 * time.Millisecond)
	assert.Zero(t, h.reboots.Load(), "[AC-020] commands.allow_reboot=false is never overridden by a command parameter")
	assert.Equal(t, 1, h.telemetryPending(), "[AC-020] telemetry is pushed so the reboot-required event can fire")
	assert.Equal(t, claimOK, h.a.claimPatchReboot(idA), "[AC-020] a vetoed reboot consumes no intent")
}

func TestRebootAuth_PF_AC020_AllowRebootTurnedOffBeforeTheRebootAborts(t *testing.T) {
	h := newAuthHarness(t, true, true)
	// The operator turns reboots off after the decision and before execReboot.
	h.a.pause = func(context.Context, time.Duration) bool {
		h.a.config.Commands.AllowReboot = false
		return true
	}

	h.runOSUpdate(true, successRun("ok"))

	require.Eventually(t, func() bool { return len(h.agentEventMessages()) == 1 }, 2*time.Second, time.Millisecond)
	assert.Zero(t, h.reboots.Load(), "[AC-020] allow_reboot is re-checked immediately before execReboot")
	assert.Contains(t, h.agentEventMessages()[0], idA)
	assert.Contains(t, h.agentEventMessages()[0], "commands.allow_reboot is off")
}

func TestRebootAuth_PF_AC021_SecondExecutionSameIDSuppressed(t *testing.T) {
	h := newAuthHarness(t, true, true)
	cr := h.a.newCmdRun(models.CommandPayload{CommandID: idA, Action: models.ActionOSUpdate})
	defer cr.cancel()

	first := successRun("first")
	h.a.decideAuthorizedReboot(context.Background(), idA, &first)
	second := successRun("second")
	h.a.decideAuthorizedReboot(context.Background(), idA, &second)

	assert.True(t, first.RebootAuth.Started, "the first decision claims the reboot")
	assert.False(t, second.RebootAuth.Started, "[AC-021] a command executed twice reboots at most once")
	assert.Equal(t, "second | Reboot authorized; reboot required after run: yes; reboot not started: suppressed (reboot already claimed for this command)", second.Message)
}

func TestRebootAuth_PF_AC035_NoTriggerWhenRebootStarts(t *testing.T) {
	h := newAuthHarness(t, true, true)

	h.runOSUpdate(true, successRun("ok"))

	require.Eventually(t, func() bool { return h.reboots.Load() == 1 }, 2*time.Second, time.Millisecond)
	assert.Zero(t, h.telemetryPending(), "[AC-035] no extra collection while the agent is about to reboot")
}

func TestOSUpdate_PF_AC035_EveryNonRebootOutcomeTriggersOneTelemetry(t *testing.T) {
	precheck := precheckFailure("OS update mode must be full, security or repair")
	cases := []struct {
		name       string
		authorized bool
		allow      bool
		required   bool
		run        commandExecResult
	}{
		{"authorized success not required", true, true, false, successRun("ok")},
		{"authorized success vetoed", true, false, true, successRun("ok")},
		{"authorized failure", true, true, true, failedRun("")},
		{"authorized precheck failure", true, true, false, precheck},
		{"unauthorized success, nothing applied", false, true, true, successRun("ok")},
		{"unauthorized failure", false, true, false, failedRun("")},
	}
	for _, tc := range cases {
		t.Run("[AC-035] "+tc.name, func(t *testing.T) {
			h := newAuthHarness(t, tc.allow, tc.required)

			h.runOSUpdate(tc.authorized, tc.run)

			assert.Equal(t, 1, h.telemetryPending(), "[AC-035] exactly one update-state refresh is requested")
			assert.Zero(t, h.reboots.Load())
		})
	}
}

func TestOSUpdate_PF_AC035_TriggersCoalesce(t *testing.T) {
	h := newAuthHarness(t, true, false)
	h.a.triggerTelemetry()
	h.a.triggerTelemetry()
	h.a.triggerTelemetry()
	assert.Equal(t, 1, h.telemetryPending(), "[AC-035] many triggers while one is pending collapse into one collection")
}

func TestRebootAuth_PF_AC037_SuffixGrammarAllReasons(t *testing.T) {
	cases := []struct {
		name    string
		outcome rebootAuthOutcome
		want    string
	}{
		{"started", rebootAuthOutcome{Required: true, Started: true}, suffixStarted},
		{"not required", rebootAuthOutcome{Reason: rebootReasonNotRequired}, suffixNotRequired},
		{"allow_reboot off", rebootAuthOutcome{Required: true, Reason: rebootReasonAllowRebootOff}, suffixAllowRebootOff},
		{"run not successful", rebootAuthOutcome{Required: true, Reason: rebootReasonRunNotSuccess},
			"Reboot authorized; reboot required after run: yes; reboot not started: run not successful"},
		{"already claimed", rebootAuthOutcome{Required: true, Reason: rebootReasonSuppressed, Detail: suppressedAlreadyClaimed},
			"Reboot authorized; reboot required after run: yes; reboot not started: suppressed (reboot already claimed for this command)"},
		{"intent not recorded", rebootAuthOutcome{Required: true, Reason: rebootReasonSuppressed, Detail: suppressedIntentNotRecorded},
			"Reboot authorized; reboot required after run: yes; reboot not started: suppressed (reboot intent could not be recorded)"},
		{"store full", rebootAuthOutcome{Required: true, Reason: rebootReasonSuppressed, Detail: suppressedIntentStoreFull},
			"Reboot authorized; reboot required after run: yes; reboot not started: suppressed (reboot intent store full)"},
		{"recovered", rebootAuthOutcome{Required: true, Reason: rebootReasonSuppressed, Detail: suppressedResultRecovered},
			"Reboot authorized; reboot required after run: yes; reboot not started: suppressed (result recovered after agent restart)"},
	}
	for _, tc := range cases {
		t.Run("[AC-037] "+tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.outcome.suffix())
		})
	}
	t.Run("[AC-037] suffix survives result truncation", func(t *testing.T) {
		long := strings.Repeat("x", models.MaxResultMsgBytes*2)
		msg := models.TruncateTail(appendRebootSuffix(long, rebootAuthOutcome{Required: true, Started: true}), models.MaxResultMsgBytes)
		assert.True(t, strings.HasSuffix(msg, suffixStarted), "the FR-023 facts are at the tail, which truncation keeps")
	})
}

func TestRebootAuth_PF_AC037_AuthorizationParamValidation(t *testing.T) {
	cases := []struct {
		name    string
		params  map[string]interface{}
		want    bool
		wantErr bool
	}{
		{"absent", map[string]interface{}{}, false, false},
		{"true", map[string]interface{}{"reboot_if_required": true}, true, false},
		{"false", map[string]interface{}{"reboot_if_required": false}, false, false},
		{"string", map[string]interface{}{"reboot_if_required": "true"}, false, true},
		{"number", map[string]interface{}{"reboot_if_required": float64(1)}, false, true},
		{"null", map[string]interface{}{"reboot_if_required": nil}, false, true},
	}
	for _, tc := range cases {
		t.Run("[AC-015] "+tc.name, func(t *testing.T) {
			got, err := rebootAuthorization(tc.params)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantErr, err != nil)
		})
	}
	t.Run("[AC-015] a malformed value is a precheck failure and never runs the update", func(t *testing.T) {
		h := newAuthHarness(t, true, true)
		cr := h.a.newCmdRun(models.CommandPayload{CommandID: idA, Action: models.ActionOSUpdate, Params: map[string]interface{}{"reboot_if_required": "yes"}})
		defer cr.cancel()
		r := h.a.executeOSUpdate(cr)
		assert.Equal(t, models.ResultStatusError, r.Status)
		assert.Equal(t, models.StepPrecheck, r.Step)
		assert.Contains(t, r.Message, "reboot_if_required must be a boolean")
	})
}

func TestRebootAuth_PF_Bound_StoreFullSuppressesWithDetail(t *testing.T) {
	h := newAuthHarness(t, true, true)
	intents := map[string]time.Duration{}
	for i := 0; i < rebootIntentCap; i++ {
		intents[uuidN(7000+i)] = 5 * time.Hour
	}
	seedIntentsAt(t, h.lifecycleHarness, intents)

	h.runOSUpdate(true, successRun("ok"))

	assert.Contains(t, h.message(), "reboot not started: suppressed (reboot intent store full)")
	time.Sleep(60 * time.Millisecond)
	assert.Zero(t, h.reboots.Load(), "a full store inside the 72h floor fails closed")
	msgs := h.agentEventMessages()
	require.Len(t, msgs, 1, "[C-2] exactly one warning event per suppressed command")
	assert.Contains(t, msgs[0], idA)
	assert.Contains(t, msgs[0], "intent store is full")
}

// seedIntentsAt writes a v2 intent store at the path the harness agent uses.
func seedIntentsAt(t *testing.T, h *lifecycleHarness, ages map[string]time.Duration) {
	t.Helper()
	path, _ := h.a.rebootIntentLocation()
	f := &intentFixture{t: t, clk: h.clk, path: path}
	f.seed(ages)
}

func TestRebootAuth_PF_C2_PersistFailureEmitsOneWarningEvent(t *testing.T) {
	t.Run("authorized path", func(t *testing.T) {
		h := newAuthHarness(t, true, true)
		h.a.rebootIntentPath = t.TempDir() // a directory: the store cannot be read

		h.runOSUpdate(true, successRun("ok"))

		assert.Contains(t, h.message(), "suppressed (reboot intent could not be recorded)")
		msgs := h.agentEventMessages()
		require.Len(t, msgs, 1, "[C-2] one event per command, not a log line only")
		assert.Contains(t, msgs[0], idA)
		assert.Zero(t, h.reboots.Load())
	})
	t.Run("automated path", func(t *testing.T) {
		h := newAuthHarness(t, true, true)
		h.a.rebootIntentPath = t.TempDir()

		h.a.maybeRebootAfterPatch(commandExecResult{RebootCommandID: idA, RebootClassApplied: []string{"linux-image-amd64"}})
		h.wait()

		msgs := h.agentEventMessages()
		require.Len(t, msgs, 1, "[C-2] the automated path emits the same warning event")
		assert.Contains(t, msgs[0], idA)
		assert.Zero(t, h.reboots.Load())
	})
	t.Run("already claimed is not a store fault", func(t *testing.T) {
		h := newAuthHarness(t, true, true)
		require.Equal(t, claimOK, h.a.claimPatchReboot(idA))
		first := successRun("ok")
		h.a.decideAuthorizedReboot(context.Background(), idA, &first)
		h.wait()
		assert.Empty(t, h.agentEventMessages(), "a duplicate is the at-most-once guard working, not a warning")
	})
}

// ownHelperFixture builds a maintenance client whose helper unit is active
// (or deactivating) while the record says which command it belongs to.
func ownHelperFixture(recordOwner string, finished bool) *maintenanceClient {
	m, rec := maintenanceFixture()
	rec.CommandID = recordOwner
	if finished {
		rec.Outcome = "success"
		rec.FinishedAt = &rec.UpdatedAt
	}
	m.current = func() (maintenance.Record, error) { return rec, nil }
	m.active = func(context.Context) (bool, error) { return true, nil }
	return m
}

func TestRebootAuth_PF_C3_OwnFinishedHelperUnitIsNotBusy(t *testing.T) {
	t.Run("authorized path proceeds", func(t *testing.T) {
		h := newAuthHarness(t, true, true)
		h.a.maintenanceClient = ownHelperFixture(idA, true)

		h.runOSUpdate(true, successRun("ok"))

		require.Eventually(t, func() bool { return h.reboots.Load() == 1 }, 2*time.Second, time.Millisecond,
			"[C-3] this command's own helper unit still winding down must not abort the reboot")
		assert.Empty(t, h.agentEventMessages())
	})
	t.Run("unit turns inactive during the wait", func(t *testing.T) {
		h := newAuthHarness(t, true, true)
		m := ownHelperFixture(idA, true)
		var polls atomic.Int32
		m.active = func(context.Context) (bool, error) { return polls.Add(1) <= 2, nil } // deactivating, then inactive
		h.a.maintenanceClient = m

		h.runOSUpdate(true, successRun("ok"))

		require.Eventually(t, func() bool { return h.reboots.Load() == 1 }, 2*time.Second, time.Millisecond)
	})
	t.Run("automated path proceeds", func(t *testing.T) {
		h := newAuthHarness(t, true, true)
		h.a.maintenanceClient = ownHelperFixture(idA, true)

		h.a.maybeRebootAfterPatch(commandExecResult{RebootCommandID: idA, RebootClassApplied: []string{"linux-image-amd64"}})

		require.Eventually(t, func() bool { return h.reboots.Load() == 1 }, 2*time.Second, time.Millisecond,
			"[C-3] the same false 'busy' is removed from the automated path (no rule change)")
	})
}

func TestRebootAuth_PF_C3_AnotherCommandsHelperAbortsWithEvent(t *testing.T) {
	h := newAuthHarness(t, true, true)
	h.a.maintenanceClient = ownHelperFixture(idB, false) // idB's worker is running

	h.runOSUpdate(true, successRun("ok"))

	require.Eventually(t, func() bool { return len(h.agentEventMessages()) == 1 }, 2*time.Second, time.Millisecond,
		"[C-3] every abort after 'reboot started: yes' emits a warning event")
	msg := h.agentEventMessages()[0]
	assert.Contains(t, msg, idA, "the event names the command")
	assert.Contains(t, msg, "was not started")
	assert.Contains(t, msg, maintenanceRunning, "the event names the reason")
	assert.Zero(t, h.reboots.Load())
	assert.Equal(t, 1, h.telemetryPending(), "telemetry is refreshed so the still-required reboot is visible")
}

func TestRebootAuth_PF_C3_EveryAbortAfterStartedEmitsAnEvent(t *testing.T) {
	t.Run("shutting down", func(t *testing.T) {
		h := newAuthHarness(t, true, true)
		h.a.pause = func(context.Context, time.Duration) bool {
			h.a.shuttingDown.Store(true)
			return true
		}
		h.runOSUpdate(true, successRun("ok"))
		require.Eventually(t, func() bool { return len(h.agentEventMessages()) == 1 }, 2*time.Second, time.Millisecond)
		assert.Contains(t, h.agentEventMessages()[0], "agent shutting down")
		assert.Zero(t, h.reboots.Load())
	})
	t.Run("reboot command fails", func(t *testing.T) {
		h := newAuthHarness(t, true, true)
		h.rebootRunnerErr = errors.New("systemctl: access denied")
		h.runOSUpdate(true, successRun("ok"))
		require.Eventually(t, func() bool { return len(h.agentEventMessages()) == 1 }, 2*time.Second, time.Millisecond)
		assert.Contains(t, h.agentEventMessages()[0], "reboot command failed")
	})
	t.Run("host lock is taken by another command", func(t *testing.T) {
		h := newAuthHarness(t, true, true)
		h.a.executor = func(cr *cmdRun) commandExecResult {
			r := h.a.attachRebootDecision(cr, true, successRun("ok"))
			// Another command grabs the host between runAction's unlock and phase B.
			inner := r.postResult
			r.postResult = func() {
				_, _, ok := h.a.locks.TryLockAll([]string{models.TargetKeyHost}, idB, models.ActionOSUpdate)
				assert.True(t, ok)
				inner()
			}
			return r
		}
		h.submit(idA, models.ActionOSUpdate, map[string]interface{}{"reboot_if_required": true})
		h.wait()
		require.Len(t, h.agentEventMessages(), 1)
		assert.Contains(t, h.agentEventMessages()[0], "host busy")
		assert.Zero(t, h.reboots.Load())
	})
}

func TestRebootAuth_PF_AC038_VermontRunReportsThenRebootsOnce(t *testing.T) {
	h := newAuthHarness(t, true, true)
	rec := maintenance.Record{
		Version: 1, CommandID: idA, Mode: "full", IncludeRebootClass: true,
		Outcome: "success", Phase: "finished", Summary: "Package state verified; maintenance completed",
		VersionChanges: []maintenance.VersionChange{{Package: "linux-image-amd64", Before: "6.12.48-1", After: "7.2.9-1"}},
	}
	run := maintenanceResult(rec)
	run.PackageManager = pmAPT
	require.Equal(t, models.ResultStatusSuccess, run.Status, "precondition: the helper reported success")

	h.runOSUpdate(true, run)

	require.Eventually(t, func() bool { return h.reboots.Load() == 1 }, 2*time.Second, time.Millisecond)
	assert.True(t, strings.HasSuffix(h.message(), suffixStarted), "the result states the reboot before it happens: %q", h.message())
	assert.True(t, h.queuedAtReboot.Load(), "result reported first, reboot second")
	assert.Equal(t, []string{pmAPT}, h.requiredPMs, "the FR-002 evaluation ran once for the apt manager")
	time.Sleep(60 * time.Millisecond)
	assert.EqualValues(t, 1, h.reboots.Load(), "reboots once")
}

func TestOSUpdate_PF_AC013_UnauthorizedRunNeverRebootsWhenRequired(t *testing.T) {
	h := newAuthHarness(t, true, true) // reboot required, allow_reboot on

	h.runOSUpdate(false, successRun("Upgraded 3 packages"))

	assert.Equal(t, "Upgraded 3 packages", h.message(), "[AC-013] an unauthorized run states no reboot facts")
	time.Sleep(60 * time.Millisecond)
	assert.Zero(t, h.reboots.Load(), "[AC-013] without the tick a run never starts a reboot, even when one is required")
	assert.Zero(t, h.requiredCheckHit.Load(), "[AC-013] the FR-002 evaluation belongs to authorized runs only")
}

func TestOSUpdate_PF_AC022_AutomatedRebootRuleUnchanged(t *testing.T) {
	t.Run("[AC-022] reboot-class applied and allow_reboot reboots", func(t *testing.T) {
		h := newAuthHarness(t, true, false) // FR-002 says "not required" - the automated rule ignores it
		r := successRun("ok")
		r.RebootClassApplied = []string{"linux-image-amd64"}

		h.runOSUpdate(false, r)

		require.Eventually(t, func() bool { return h.reboots.Load() == 1 }, 2*time.Second, time.Millisecond)
	})
	t.Run("[AC-022] nothing applied does not reboot", func(t *testing.T) {
		h := newAuthHarness(t, true, true)
		h.runOSUpdate(false, successRun("ok"))
		time.Sleep(60 * time.Millisecond)
		assert.Zero(t, h.reboots.Load())
	})
	t.Run("[AC-022] allow_reboot off does not reboot", func(t *testing.T) {
		h := newAuthHarness(t, false, true)
		r := successRun("ok")
		r.RebootClassApplied = []string{"linux-image-amd64"}
		h.runOSUpdate(false, r)
		time.Sleep(60 * time.Millisecond)
		assert.Zero(t, h.reboots.Load())
		assert.Equal(t, 1, h.telemetryPending())
	})
}

func TestRecoverMaintenance_PF_AC021_RecoveredAuthorizedResultNeverReboots(t *testing.T) {
	h := newAuthHarness(t, true, true)
	m, rec := maintenanceFixture()
	rec.Outcome = "success"
	rec.Phase = "finished"
	rec.Summary = "Package state verified; maintenance completed"
	rec.FinishedAt = &rec.UpdatedAt
	rec.IncludeRebootClass = true
	rec.VersionChanges = []maintenance.VersionChange{{Package: "linux-image-amd64", Before: "1", After: "2"}}
	m.current = func() (maintenance.Record, error) { return rec, nil }
	m.terminal = func(string) (maintenance.Record, error) { return rec, nil }
	h.a.maintenanceClient = m
	marker := inflightMarker{Action: models.ActionOSUpdate, StartedAt: rec.StartedAt, RebootIfRequired: true}
	require.NoError(t, h.a.inflight.Put(idA, marker))

	require.True(t, h.a.recoverMaintenanceCommand(idA, marker))
	h.wait()

	msg := h.resultFor(idA).Message
	assert.Contains(t, msg, "Reboot authorized; reboot required after run: yes; reboot not started: suppressed (result recovered after agent restart)")
	time.Sleep(60 * time.Millisecond)
	assert.Zero(t, h.reboots.Load(), "[AC-021] a recovered result never reboots")
	assert.Equal(t, 1, h.telemetryPending(), "recovery refreshes update state once")
}

func TestRecoverMaintenance_PF_AC021_UnauthorizedRecoveryAddsNoSuffix(t *testing.T) {
	h := newAuthHarness(t, true, true)
	m, rec := maintenanceFixture()
	rec.Outcome = "success"
	rec.Phase = "finished"
	rec.Summary = "Package state verified; maintenance completed"
	rec.FinishedAt = &rec.UpdatedAt
	m.current = func() (maintenance.Record, error) { return rec, nil }
	m.terminal = func(string) (maintenance.Record, error) { return rec, nil }
	h.a.maintenanceClient = m
	marker := inflightMarker{Action: models.ActionOSUpdate, StartedAt: rec.StartedAt}
	require.NoError(t, h.a.inflight.Put(idA, marker))

	require.True(t, h.a.recoverMaintenanceCommand(idA, marker))
	h.wait()

	assert.NotContains(t, h.resultFor(idA).Message, "Reboot authorized")
}

func TestRebootAuth_PF_AC021_MarkerCarriesAuthorizationAtIntake(t *testing.T) {
	h := newAuthHarness(t, true, true)
	block := make(chan struct{})
	h.a.executor = func(cr *cmdRun) commandExecResult {
		<-block
		return successRun("ok")
	}
	h.submit(idA, models.ActionOSUpdate, map[string]interface{}{"reboot_if_required": true})
	h.submit(idB, models.ActionOSUpdate, map[string]interface{}{"mode": "full"})

	markerA, okA := h.a.inflight.Get(idA)
	markerB, okB := h.a.inflight.Get(idB)
	close(block)
	h.wait()

	require.True(t, okA && okB)
	assert.True(t, markerA.RebootIfRequired, "an authorized os_update is marked so recovery can state it")
	assert.False(t, markerB.RebootIfRequired)
}

func TestMaintenanceBlockerFor_PF_C3_StrictWithoutOwnID(t *testing.T) {
	a := &Agent{maintenanceClient: ownHelperFixture(idA, true)}
	assert.Equal(t, maintenanceRunning, a.maintenanceBlocker(context.Background()),
		"the strict check (no owner) is unchanged: an active unit is always busy")
	assert.Empty(t, a.maintenanceBlockerFor(context.Background(), idA), "only the owner's finished run is excused")
	assert.Equal(t, maintenanceRunning, a.maintenanceBlockerFor(context.Background(), idB), "someone else's id is not excused")
}

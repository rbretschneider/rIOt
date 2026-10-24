package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// lifecycleHarness is an Agent with the command system wired to fakes: a fake
// clock, a fake transport (WebSocket up by default), and file-backed state in a
// temp directory.
type lifecycleHarness struct {
	t   *testing.T
	a   *Agent
	tr  *fakeTransport
	clk *fakeClock
	fs  *faultFS
	log *eventLog

	seenPath, inflightPath, outboxPath string
}

func newLifecycleHarness(t *testing.T) *lifecycleHarness {
	t.Helper()
	return newLifecycleHarnessAt(t, t.TempDir())
}

// newLifecycleHarnessAt builds a harness over an existing state directory, to
// model an agent restart over the same files.
func newLifecycleHarnessAt(t *testing.T, dir string) *lifecycleHarness {
	t.Helper()
	h := &lifecycleHarness{
		t:            t,
		clk:          newFakeClock(),
		fs:           newFaultFS(),
		log:          &eventLog{},
		seenPath:     filepath.Join(dir, "seen-commands.json"),
		inflightPath: filepath.Join(dir, "inflight-commands.json"),
		outboxPath:   filepath.Join(dir, "command-outbox.json"),
	}
	h.tr = &fakeTransport{wsUp: true, log: h.log}
	h.a = &Agent{config: &Config{}, telemetryNow: make(chan struct{}, 1)}
	h.a.config.Agent.DeviceID = "dev-1"
	// Tests never talk to a real Docker daemon: Docker is unavailable unless a
	// test installs a fake.
	h.a.dockerFactory = func() (dockerAPI, error) { return nil, errors.New("docker is not available in this test") }
	// REBOOT-DPKG-001: no test reads the real /proc. The dpkg lock reads as free
	// unless a test installs its own scripted probe.
	h.a.dpkgLockProbe = freeDpkgLockProbe

	ctx, cancel := context.WithCancel(context.Background())
	h.a.cmdCtx, h.a.cmdCancel = context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		h.a.cmdCancel()
	})
	h.a.setupCommandSystem(ctx, commandSystemDeps{
		clk: h.clk, fs: h.fs, tr: h.tr,
		outboxPath: h.outboxPath, seenPath: h.seenPath, inflightPath: h.inflightPath,
	})
	return h
}

// submit delivers a command through the real intake.
func (h *lifecycleHarness) submit(id, action string, params map[string]interface{}) {
	h.t.Helper()
	h.a.submitCommand(models.CommandPayload{CommandID: id, Action: action, Params: params}, "ws")
}

// wait blocks until every submitted command has finished and the notifier has
// drained.
func (h *lifecycleHarness) wait() {
	h.t.Helper()
	h.a.cmdWG.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.a.notify.Flush(ctx)
}

// deliver runs one outbox pass.
func (h *lifecycleHarness) deliver() { h.a.out.processDue(context.Background()) }

// results returns every command_result written over the fake WebSocket.
func (h *lifecycleHarness) results() []models.CommandResult {
	h.t.Helper()
	var out []models.CommandResult
	for _, m := range h.tr.wsMessages("command_result") {
		var r models.CommandResult
		require.NoError(h.t, json.Unmarshal(m.Data, &r))
		out = append(out, r)
	}
	return out
}

// resultFor returns the queued (not yet delivered) or delivered result for id.
func (h *lifecycleHarness) resultFor(id string) models.CommandResult {
	h.t.Helper()
	for _, e := range h.a.out.Snapshot() {
		if e.CommandID == id {
			return e.Result
		}
	}
	for _, r := range h.results() {
		if r.CommandID == id {
			return r
		}
	}
	h.t.Fatalf("no result queued or delivered for command %s", id)
	return models.CommandResult{}
}

// exec installs an executor and returns a counter of invocations.
func (h *lifecycleHarness) exec(fn func(cr *cmdRun) commandExecResult) *atomic.Int32 {
	var n atomic.Int32
	h.a.executor = func(cr *cmdRun) commandExecResult {
		n.Add(1)
		return fn(cr)
	}
	return &n
}

func okResult() commandExecResult {
	return commandExecResult{Status: models.ResultStatusSuccess, Message: "done"}
}

// AC-067 (FR-057): the same command ID delivered by WS and by heartbeat runs
// once.
func TestCL_AC067_SameIDViaWSAndHeartbeatRunsOnce(t *testing.T) {
	t.Run("sequential redelivery", func(t *testing.T) {
		h := newLifecycleHarness(t)
		runs := h.exec(func(*cmdRun) commandExecResult { return okResult() })

		h.a.submitCommand(models.CommandPayload{CommandID: idA, Action: "fetch_logs"}, "ws")
		h.a.submitCommand(models.CommandPayload{CommandID: idA, Action: "fetch_logs"}, "heartbeat")
		h.wait()

		assert.Equal(t, int32(1), runs.Load(), "[AC-067] the second delivery of the same ID must be dropped")
		assert.Equal(t, 1, h.a.out.Len(), "[AC-067] exactly one result must be queued")
	})

	t.Run("simultaneous delivery", func(t *testing.T) {
		h := newLifecycleHarness(t)
		runs := h.exec(func(*cmdRun) commandExecResult { return okResult() })

		var wg sync.WaitGroup
		for _, via := range []string{"ws", "heartbeat"} {
			wg.Add(1)
			go func(via string) {
				defer wg.Done()
				h.a.submitCommand(models.CommandPayload{CommandID: idA, Action: "fetch_logs"}, via)
			}(via)
		}
		wg.Wait()
		h.wait()

		assert.Equal(t, int32(1), runs.Load(), "[AC-067] concurrent WS and heartbeat delivery must run the command once")
	})

	t.Run("redelivery after restart", func(t *testing.T) {
		h := newLifecycleHarness(t)
		runs := h.exec(func(*cmdRun) commandExecResult { return okResult() })
		h.submit(idA, "fetch_logs", nil)
		h.wait()

		// A new process over the same state files.
		restarted := newLifecycleHarnessAt(t, filepath.Dir(h.seenPath))
		runs2 := restarted.exec(func(*cmdRun) commandExecResult { return okResult() })
		restarted.submit(idA, "fetch_logs", nil)
		restarted.wait()

		assert.Equal(t, int32(1), runs.Load())
		assert.Equal(t, int32(0), runs2.Load(), "[AC-067] a restarted agent must not run an ID it has already seen")
	})
}

// AC-092 (FR-075): the ack is written before the first execution step.
func TestCL_AC092_AckBeforeFirstStep(t *testing.T) {
	t.Run("over the WebSocket", func(t *testing.T) {
		h := newLifecycleHarness(t)
		h.exec(func(*cmdRun) commandExecResult {
			h.log.add("exec:first-step")
			return okResult()
		})

		h.submit(idA, "fetch_logs", nil)
		h.wait()

		ack, step := h.log.index("ws:command_ack"), h.log.index("exec:first-step")
		require.GreaterOrEqual(t, ack, 0, "[AC-092] an ack must be sent")
		require.GreaterOrEqual(t, step, 0)
		assert.Less(t, ack, step, "[AC-092] the ack must be on the wire before the first step runs")
	})

	t.Run("over HTTP when the WebSocket is down", func(t *testing.T) {
		h := newLifecycleHarness(t)
		h.tr.wsUp = false
		h.exec(func(*cmdRun) commandExecResult {
			h.log.add("exec:first-step")
			return okResult()
		})

		h.submit(idA, "fetch_logs", nil)
		h.wait()

		ack, step := h.log.index("post:ack"), h.log.index("exec:first-step")
		require.GreaterOrEqual(t, ack, 0, "[AC-092] without a WebSocket the ack goes over HTTP")
		assert.Less(t, ack, step, "[AC-092] the HTTP ack must also precede the first step")
		assert.Equal(t, idA, h.tr.posts[0].CommandID)
	})

	t.Run("skipped against a legacy server", func(t *testing.T) {
		h := newLifecycleHarness(t)
		h.a.out.enterLegacy()
		h.exec(func(*cmdRun) commandExecResult { return okResult() })

		h.submit(idA, "fetch_logs", nil)
		h.wait()

		assert.Equal(t, -1, h.log.indexPrefix("ws:command_ack"), "[AC-092] a legacy server must not receive acks")
		assert.Equal(t, 0, h.tr.postCount(), "[AC-092] and no HTTP report may be attempted")
	})
}

// AC-099 (FR-081): the final result keeps the legacy status vocabulary and adds
// step, outcome, stderr and reason.
func TestCL_AC099_FinalResultLegacyStatusPlusFields(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		h := newLifecycleHarness(t)
		h.exec(func(cr *cmdRun) commandExecResult {
			cr.Step(models.StepPull)
			cr.Verifying(models.StepVerify)
			return okResult()
		})
		h.submit(idA, "docker_update", nil)
		h.wait()
		h.deliver()

		msgs := h.tr.wsMessages("command_result")
		require.Len(t, msgs, 1, "[AC-099] the result must go over the WebSocket")
		var raw map[string]interface{}
		require.NoError(t, json.Unmarshal(msgs[0].Data, &raw))
		assert.Equal(t, "success", raw["status"], "[AC-099] status stays in the legacy vocabulary")
		assert.Equal(t, "succeeded", raw["outcome"], "[AC-099] the canonical outcome is added")
		assert.Equal(t, "verify", raw["step"], "[AC-099] the last step is reported on success")
		assert.Equal(t, idA, raw["command_id"])
	})

	t.Run("failure", func(t *testing.T) {
		h := newLifecycleHarness(t)
		h.exec(func(cr *cmdRun) commandExecResult {
			cr.Step(models.StepPull)
			return commandExecResult{Status: models.ResultStatusError, Message: "pull failed", Step: models.StepPull,
				Stderr: "denied", Reason: models.ReasonTimeout}
		})
		h.submit(idA, "docker_update", nil)
		h.wait()
		h.deliver()

		r := h.results()[0]
		assert.Equal(t, "error", r.Status, "[AC-099] a failure keeps status=error")
		assert.Equal(t, "failed", r.Outcome)
		assert.Equal(t, "pull", r.Step)
		assert.Equal(t, "denied", r.Stderr)
		assert.Equal(t, "timeout", r.Reason)
	})
}

// AC-094 (FR-077): a failed result carries the failing step and at most the
// last 8 KiB of stderr.
func TestCL_AC094_StderrTail8KiBAndFailureStep(t *testing.T) {
	h := newLifecycleHarness(t)
	long := strings.Repeat("a", 12*1024) + strings.Repeat("b", 8*1024)
	h.exec(func(*cmdRun) commandExecResult {
		return commandExecResult{Status: models.ResultStatusError, Message: "boom", Step: models.StepRecreate, Stderr: long}
	})

	h.submit(idA, "docker_update", nil)
	h.wait()

	r := h.resultFor(idA)
	assert.Len(t, r.Stderr, models.MaxStderrTailBytes, "[AC-094] stderr must be cut to 8 KiB")
	assert.Equal(t, strings.Repeat("b", 8*1024), r.Stderr, "[AC-094] the tail, not the head, must be kept")
	assert.Equal(t, models.StepRecreate, r.Step, "[AC-094] the failing step must be set")
}

// AC-021 (FR-019): an in-flight docker_update at step recreate is reported as
// failed with that step after a restart.
func TestCL_AC021_InterruptedDockerUpdateReportedFailed(t *testing.T) {
	h := newLifecycleHarness(t)
	require.NoError(t, h.a.inflight.Put(idA, inflightMarker{
		Action: "docker_update", Target: "web", Step: models.StepRecreate, StartedAt: h.clk.Now()}))

	h.a.recoverInterruptedCommands(context.Background())

	r := h.resultFor(idA)
	assert.Equal(t, "error", r.Status)
	assert.Equal(t, "failed", r.Outcome)
	assert.Equal(t, "recreate", r.Step, "[AC-021] the last recorded step must be reported")
	assert.Equal(t, "agent restarted during execution; outcome unknown", r.Message,
		"[AC-021] the message is fixed by FR-019")
	assert.Equal(t, models.ReasonRestarted, r.Reason)
	_, stillMarked := h.a.inflight.Get(idA)
	assert.False(t, stillMarked, "[AC-021] a reported marker must be cleared")

	// Recovering again must not report twice.
	h.a.recoverInterruptedCommands(context.Background())
	assert.Equal(t, 1, h.a.out.Len(), "[AC-021] a command is reported once")
}

// AC-021: a marker whose result is already queued is not reported again.
func TestCL_AC021_MarkerWithQueuedResultNotReportedTwice(t *testing.T) {
	h := newLifecycleHarness(t)
	require.NoError(t, h.a.out.Add(models.CommandResult{CommandID: idA, Status: "success", Message: "real result"}))
	require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: "os_update", StartedAt: h.clk.Now()}))

	h.a.recoverInterruptedCommands(context.Background())

	assert.Equal(t, "real result", h.resultFor(idA).Message, "[AC-021] the genuine result must not be overwritten")
}

// AC-022 (FR-019): self-update, reboot and shutdown markers are not reported as
// failures; they are simply cleared.
func TestCL_AC022_SelfUpdateMarkerNotReported(t *testing.T) {
	for _, action := range []string{models.ActionAgentUpdate, models.ActionReboot, models.ActionShutdown} {
		t.Run(action, func(t *testing.T) {
			h := newLifecycleHarness(t)
			require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: action, StartedAt: h.clk.Now()}))

			h.a.recoverInterruptedCommands(context.Background())

			assert.Equal(t, 0, h.a.out.Len(), "[AC-022] no 'agent restarted' failure for %s", action)
			_, stillMarked := h.a.inflight.Get(idA)
			assert.False(t, stillMarked, "[AC-022] the marker must be cleared")
		})
	}
}

// AC-053 (FR-046): lifecycle events and the final result still reach the
// transport after the operation's context has expired.
func TestCL_AC053_EventAndResultAfterOpCtxExpired(t *testing.T) {
	h := newLifecycleHarness(t)
	h.exec(func(cr *cmdRun) commandExecResult {
		cr.cancel() // the operation's own context is now dead
		<-cr.ctx.Done()
		cr.Event("web", "nginx:latest", "update_failed")
		return commandExecResult{Status: models.ResultStatusError, Message: "pull timed out", Step: models.StepPull}
	})

	h.submit(idA, "docker_update", nil)
	h.wait()
	h.deliver()

	assert.Contains(t, h.tr.dockerEventActions(), "update_failed",
		"[AC-053] the update_failed event must be sent on a fresh context")
	assert.NotEmpty(t, h.tr.agentEvents, "[AC-053] the command-failed event must be sent too")
	require.Len(t, h.results(), 1, "[AC-053] the final result must be delivered")
	assert.Equal(t, "pull", h.results()[0].Step)
}

// AC-066 (FR-056): a panicking command releases its locks, reports a failure and
// does not stop the next command for the same target.
func TestCL_AC066_LockReleasedAfterPanickingCommand(t *testing.T) {
	h := newLifecycleHarness(t)
	holder := ""
	h.exec(func(cr *cmdRun) commandExecResult {
		if _, h2, ok := cr.lock("docker:container:web"); !ok {
			return conflictResult("docker:container:web", h2)
		}
		if cr.id == idA {
			panic("kaboom")
		}
		holder = cr.id
		return okResult()
	})

	h.submit(idA, "docker_update", nil)
	h.wait()
	h.submit(idB, "docker_update", nil)
	h.wait()

	first := h.resultFor(idA)
	assert.Equal(t, "error", first.Status, "[AC-066] a recovered panic must produce a failed result")
	assert.Contains(t, first.Message, "kaboom")
	assert.Equal(t, idB, holder, "[AC-066] the next command must acquire the lock the panicking one held")
	assert.Equal(t, "success", h.resultFor(idB).Status)
	_, held := h.a.locks.Holder("docker:container:web")
	assert.False(t, held, "[AC-066] no lock may remain after both commands finished")
}

// AC-063 (FR-055) at the lifecycle level: a conflicting command fails at
// precheck with the holder's ID.
func TestCL_AC063_ConflictingHostCommandFailsPrecheck(t *testing.T) {
	h := newLifecycleHarness(t)
	release := make(chan struct{})
	started := make(chan struct{})
	h.exec(func(cr *cmdRun) commandExecResult {
		if cr.id == idA {
			if _, _, ok := cr.lock("host"); !ok {
				return okResult()
			}
			close(started)
			<-release
			return okResult()
		}
		key, holder, ok := cr.lock("host")
		if ok {
			return okResult()
		}
		return conflictResult(key, holder)
	})

	h.submit(idA, "reboot", nil)
	<-started
	h.submit(idB, "reboot", nil)
	// idB finishes immediately; idA is still running.
	require.Eventually(t, func() bool { return h.a.out.Has(idB) }, 5*time.Second, time.Millisecond)
	close(release)
	h.wait()

	r := h.resultFor(idB)
	assert.Equal(t, "error", r.Status)
	assert.Equal(t, models.StepPrecheck, r.Step, "[AC-063] a conflict fails at precheck")
	assert.Equal(t, models.ReasonConflict, r.Reason)
	assert.Contains(t, r.Message, "already in progress", "[AC-063]")
	assert.Contains(t, r.Message, idA, "[AC-063] the message must name the holding command")
}

// SEC C-4(c): agent_update and agent_uninstall are refused while a Docker lock
// is held, with the real action dispatcher (nothing may run).
func TestCL_SEC004_AgentUpdateRejectedWhileDockerLockHeld(t *testing.T) {
	assertRefusedWhileDockerLocked(t, models.ActionAgentUpdate)
}

func TestCL_SEC004_AgentUninstallRejectedWhileDockerLockHeld(t *testing.T) {
	assertRefusedWhileDockerLocked(t, models.ActionAgentUninstall)
}

func assertRefusedWhileDockerLocked(t *testing.T, action string) {
	t.Helper()
	h := newLifecycleHarness(t)
	_, _, ok := h.a.locks.TryLockAll([]string{"docker:container:web"}, idB, "docker_update")
	require.True(t, ok)

	// No executor override: the real executeAction runs. The Agent has no HTTP
	// client, so any attempt to actually update or uninstall would fail loudly.
	h.submit(idA, action, nil)
	h.wait()

	r := h.resultFor(idA)
	assert.Equal(t, "error", r.Status, "[SEC-004] %s must be refused", action)
	assert.Equal(t, models.StepPrecheck, r.Step)
	assert.Equal(t, models.ReasonConflict, r.Reason)
	assert.Equal(t, "docker update in progress for web (command "+idB+"); refusing agent restart", r.Message,
		"[SEC-004] the message must name the Docker target and its command")
	_, held := h.a.locks.Holder("host")
	assert.False(t, held, "[SEC-004] the host lock must be released after the refusal")
}

// SEC C-4(d): after SIGTERM nothing new starts; the refusal is queued for
// delivery after the restart.
func TestCL_SEC004_NoNewCommandsAcceptedAfterSIGTERM(t *testing.T) {
	h := newLifecycleHarness(t)
	runs := h.exec(func(*cmdRun) commandExecResult { return okResult() })
	h.a.shuttingDown.Store(true)

	h.submit(idA, "docker_update", nil)
	h.wait()

	assert.Equal(t, int32(0), runs.Load(), "[SEC-004] a command received during shutdown must not execute")
	r := h.resultFor(idA)
	assert.Equal(t, "error", r.Status)
	assert.Equal(t, models.StepPrecheck, r.Step)
	assert.Equal(t, models.ReasonShutdown, r.Reason)
	assert.Equal(t, "agent shutting down", r.Message)
	assert.True(t, h.a.seen.Contains(idA), "[SEC-004] the refused ID is recorded so a redelivery is not run later")
	assert.Equal(t, -1, h.log.indexPrefix("ws:command_ack"), "[SEC-004] a refused command is not acknowledged")
}

// SEC C-4(d): shutdown waits for running commands, then cancels them when the
// grace period runs out.
func TestCL_SEC004_ShutdownWaitsForCommandsThenCancels(t *testing.T) {
	t.Run("command finishes inside the grace period", func(t *testing.T) {
		h := newLifecycleHarness(t)
		release := make(chan struct{})
		h.exec(func(*cmdRun) commandExecResult {
			<-release
			return okResult()
		})
		h.submit(idA, "fetch_logs", nil)

		go func() {
			// let waitForCommands start waiting, then finish the command
			<-h.clk.timerCreated
			close(release)
		}()
		assert.True(t, h.a.waitForCommands(models.AgentShutdownGrace), "[SEC-004] a command that finishes in time is awaited")
	})

	t.Run("grace expires and commands are cancelled", func(t *testing.T) {
		captureLogs(t)
		h := newLifecycleHarness(t)
		release := make(chan struct{})
		h.exec(func(cr *cmdRun) commandExecResult {
			<-release
			return okResult()
		})
		h.submit(idA, "fetch_logs", nil)

		done := make(chan bool, 1)
		go func() { done <- h.a.waitForCommands(models.AgentShutdownGrace) }()
		<-h.clk.timerCreated
		h.clk.Advance(models.AgentShutdownGrace)

		assert.False(t, <-done, "[SEC-004] the wait must give up after the 20s grace period")
		assert.Error(t, h.a.cmdCtx.Err(), "[SEC-004] running commands are cancelled when the grace expires")
		close(release)
		h.a.cmdWG.Wait()
	})
}

// SEC C-7: a command ID that is not a canonical UUID is never executed, never
// stored and never reported, on both delivery paths; the raw value is not logged.
func TestCL_SEC007_NonUUIDCommandIDNotExecuted(t *testing.T) {
	hostile := []struct{ name, id string }{
		{"path traversal", "../../logs"},
		{"empty", ""},
		{"uppercase uuid", strings.ToUpper("abcdefab-cdef-4bcd-8fab-cdefabcdefab")},
		{"trailing character", idA + "x"},
		{"1 MiB id", strings.Repeat("a", 1<<20)},
	}
	for _, tc := range hostile {
		for _, via := range []string{"ws", "heartbeat"} {
			t.Run(tc.name+" via "+via, func(t *testing.T) {
				logs := captureLogs(t)
				h := newLifecycleHarness(t)
				runs := h.exec(func(*cmdRun) commandExecResult { return okResult() })
				payload := models.CommandPayload{CommandID: tc.id, Action: "fetch_logs"}

				if via == "ws" {
					data, err := json.Marshal(payload)
					require.NoError(t, err)
					h.a.handleCommandMessage(AgentWSMessage{Type: "command", Data: data}, "ws")
				} else {
					h.a.submitCommand(payload, "heartbeat")
				}
				h.wait()

				assert.Equal(t, int32(0), runs.Load(), "[SEC-007] a non-UUID ID must not execute")
				assert.Equal(t, 0, h.a.out.Len(), "[SEC-007] nothing may reach the outbox")
				assert.Empty(t, h.a.seen.seen, "[SEC-007] the seen store must not record it")
				assert.Empty(t, h.a.inflight.All(), "[SEC-007] no marker may be written")
				assert.Empty(t, h.tr.wsSent, "[SEC-007] no report may be sent over WS")
				assert.Equal(t, 0, h.tr.postCount(), "[SEC-007] no report may be sent over HTTP")
				if len(tc.id) > 8 {
					assert.NotContains(t, logs.String(), tc.id, "[SEC-007] the raw value must not be logged")
				}
				assert.Contains(t, logs.String(), "invalid command_id from server", "[SEC-007] a Warn must be logged")
			})
		}
	}
}

// SEC C-8: when the seen ID cannot be persisted, power and patch actions fail
// closed at precheck and do not execute; other actions run degraded.
func TestCL_SEC008_SeenPersistFailureFailsPowerActionsClosed(t *testing.T) {
	for i, action := range []string{models.ActionReboot, models.ActionShutdown, models.ActionOSUpdate, models.ActionAgentUpdate} {
		t.Run(action, func(t *testing.T) {
			captureLogs(t)
			h := newLifecycleHarness(t)
			h.fs.failOn("rename", errors.New("read-only file system"))
			runs := h.exec(func(*cmdRun) commandExecResult { return okResult() })
			id := uuidN(100 + i)

			h.submit(id, action, nil)
			h.wait()

			assert.Equal(t, int32(0), runs.Load(), "[SEC-008] %s must not execute without a durable dedupe record", action)
			r := h.resultFor(id)
			assert.Equal(t, "error", r.Status)
			assert.Equal(t, models.StepPrecheck, r.Step, "[SEC-008] the failure is at precheck")
			assert.Equal(t, models.ReasonPersistence, r.Reason)
			assert.Equal(t, "command dedupe store unavailable; refusing to run "+action, r.Message)
		})
	}

	t.Run("non-critical action runs degraded", func(t *testing.T) {
		logs := captureLogs(t)
		h := newLifecycleHarness(t)
		h.fs.failOn("rename", errors.New("read-only file system"))
		runs := h.exec(func(*cmdRun) commandExecResult { return okResult() })

		h.submit(idA, "fetch_logs", nil)
		h.wait()

		assert.Equal(t, int32(1), runs.Load(), "[SEC-008] a non-critical action may run without a durable record")
		assert.Contains(t, logs.String(), "level=ERROR", "[SEC-008] the degraded mode must be logged at Error")
		assert.Equal(t, "success", h.resultFor(idA).Status)
	})
}

// SEC C-8: the seen ID is on disk (file and directory fsynced) before the
// action runs.
func TestCL_SEC008_SeenPersistedBeforeExecute(t *testing.T) {
	h := newLifecycleHarness(t)
	var opsAtExec []string
	var seenOnDisk bool
	h.exec(func(*cmdRun) commandExecResult {
		opsAtExec = h.fs.ops()
		data, err := os.ReadFile(h.seenPath)
		seenOnDisk = err == nil && strings.Contains(string(data), idA)
		return okResult()
	})

	h.submit(idA, "reboot", nil)
	h.wait()

	assert.True(t, seenOnDisk, "[SEC-008] the seen file must contain the ID before the action runs")
	rename, syncdir := -1, -1
	for i, op := range opsAtExec {
		if op == "rename" && rename < 0 {
			rename = i
		}
		if op == "syncdir" && rename >= 0 && i > rename && syncdir < 0 {
			syncdir = i
		}
	}
	assert.GreaterOrEqual(t, rename, 0, "[SEC-008] the seen file must be renamed into place before execution")
	assert.Greater(t, syncdir, rename, "[SEC-008] the directory must be fsynced after the rename and before execution")
}

// FR-018: results are persisted before the first delivery attempt, so a result
// produced while the WebSocket is down survives and is delivered by HTTP.
func TestCL_AC016_ResultOfDisconnectedCommandPostedOverHTTP(t *testing.T) {
	h := newLifecycleHarness(t)
	h.tr.wsUp = false
	h.exec(func(*cmdRun) commandExecResult { return okResult() })

	h.submit(idA, "fetch_logs", nil)
	h.wait()
	require.True(t, h.a.out.Has(idA), "[AC-016] the result is queued before delivery")
	h.deliver()

	var posted []string
	for _, p := range h.tr.posts {
		posted = append(posted, string(p.Report.Kind))
	}
	assert.Equal(t, []string{"ack", "result"}, posted, "[AC-016] ack then result go over HTTP")
	assert.False(t, h.a.out.Has(idA), "[AC-016] the entry is removed once the server confirmed it")
}

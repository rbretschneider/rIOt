package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Added by QA Engineer (REG-HARDEN-001).
//
// These tests probe edge cases the story's own tests leave implicit: body-size
// boundary, hostile device_id values on the new-device path, one-time
// bootstrap semantics of the registration key, mixed-identity handler flows,
// and handler-level timestamp boundaries (exact allowance, +1ns, zero, far past,
// far future).

// paddedRegBody returns a valid registration JSON document of exactly n bytes.
func paddedRegBody(t *testing.T, n int) string {
	t.Helper()
	const head = `{"hostname":"`
	const tail = `","arch":"x"}`
	pad := n - len(head) - len(tail)
	require.Positive(t, pad)
	body := head + strings.Repeat("a", pad) + tail
	require.Len(t, body, n)
	return body
}

// Covers AC-019 (boundary): exactly 1 MiB is parsed, 1 MiB + 1 byte is 400.
func TestRegisterDevice_QA_AC019_BodySizeBoundary(t *testing.T) {
	t.Run("[AC-019] body of exactly maxRegistrationBodyBytes is accepted", func(t *testing.T) {
		f := newRegisterFixture(t, true, false)
		rec := f.post(t, paddedRegBody(t, maxRegistrationBodyBytes), "", nil)
		assert.Equal(t, http.StatusCreated, rec.Code, "body at the cap must still be parsed; got: %.200s", rec.Body.String())
	})
	t.Run("[AC-019] body of maxRegistrationBodyBytes+1 is 400 and creates nothing", func(t *testing.T) {
		f := newRegisterFixture(t, false, false)
		rec := f.post(t, paddedRegBody(t, maxRegistrationBodyBytes+1), "", nil)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Empty(t, f.dev.Devices, "no device may be created")
		assert.Empty(t, f.dev.APIKeys, "no key may be created")
	})
}

// Covers AC-015 / AC-020 / C1: hostile device_id values on an open server (no
// registration key). The supplied ID must never become the device's ID, never
// resolve to an existing device, and must never reach the log verbatim.
func TestRegisterDevice_QA_AC015_HostileDeviceID_OpenServer(t *testing.T) {
	// A victim whose ID contains letters, so upper-casing it yields a different string.
	const regTestCaseVictim = "abcdef01-2345-4678-89ab-cdef01234567"
	cases := map[string]string{
		"leading NUL":      "\u0000" + regTestD1,
		"embedded NUL":     regTestD1[:8] + "\u0000" + regTestD1[8:],
		"trailing newline": regTestD1 + "\n",
		"trailing space":   regTestD1 + " ",
		"upper-cased UUID": strings.ToUpper(regTestCaseVictim),
		"100 KiB string":   strings.Repeat("z", 100*1024),
		"SQL injection":    "x'; DROP TABLE devices; --",
		"unicode":          "デバイス-‮-\U0001F600",
	}
	for name, id := range cases {
		id := id
		t.Run("[AC-015] "+name, func(t *testing.T) {
			buf := captureLogs(t)
			f := newRegisterFixture(t, true, false)
			f.seedDevice(regTestD1, "victim", models.DeviceStatusOffline, []string{"keep"})
			f.seedDevice(regTestCaseVictim, "case-victim", models.DeviceStatusOffline, nil)
			f.seedKey("riot_k1", regTestD1)
			victimBefore := fmt.Sprintf("%+v", *f.dev.Devices[regTestD1])

			rec := f.post(t, regBody(id, "attacker", nil), "", nil)

			// An unresolvable ID follows the documented open-registration policy: a
			// brand-new device is created under a server-generated ID.
			require.Equal(t, http.StatusCreated, rec.Code, "body: %.200s", rec.Body.String())
			resp := decodeRegResp(t, rec)
			assert.NotEqual(t, id, resp.DeviceID, "the supplied device_id must be ignored")
			assert.NotEqual(t, regTestD1, resp.DeviceID)
			assert.Equal(t, victimBefore, fmt.Sprintf("%+v", *f.dev.Devices[regTestD1]), "victim must be untouched")
			assert.Equal(t, []string{"riot_k1"}, f.keysOf(regTestD1), "victim's key set must be untouched")
			assert.NotContains(t, buf.String(), `\u0000`)
		})
	}
}

// Covers AC-016 / AC-020 / C1: hostile device_id on a registration-key server is
// a 401 whose single log line has no control characters and no echoed payload.
func TestRegisterDevice_QA_AC020_HostileDeviceID_RegKeyServer_OneCleanLogLine(t *testing.T) {
	for _, id := range []string{
		"\u0000", "\u0000x", "a\u0000b", "line1\nline2", "\r\n", "\x1b[2J", strings.Repeat("\u0000", 4096),
	} {
		id := id
		t.Run(fmt.Sprintf("[AC-020] %q", truncateForName(id)), func(t *testing.T) {
			buf := captureLogs(t)
			f := newRegisterFixture(t, false, false)
			f.setRegKey(regTestRegKey)
			before := f.state()

			rec := f.post(t, regBody(id, "x", nil), regTestPresentedK, nil)

			assertReject401(t, rec)
			assertUnchanged(t, before, f.state(), "hostile device_id")
			raw := buf.String()
			assert.Equal(t, 1, strings.Count(raw, "\n"), "exactly one physical log line: %q", raw)
			assert.NotContains(t, raw, `\u0000`)
			assert.NotContains(t, raw, `\u001b`)
			assert.NotContains(t, raw, `\n`+"line2", "no injected content")
			assert.Contains(t, raw, `"device_id":"[invalid]"`)
		})
	}
}

func truncateForName(s string) string {
	if len(s) > 12 {
		return s[:12] + "..."
	}
	return s
}

// Covers AC-006 / AC-007 / BR-003: the registration key bootstraps a key exactly
// once. After the first success the device has keys, so the same request is a 401.
func TestRegisterDevice_QA_AC007_RegKeyBootstrapIsOneShot(t *testing.T) {
	f := newRegisterFixture(t, true, false)
	f.setRegKey(regTestRegKey)
	f.seedDevice(regTestD1, "enrolled", models.DeviceStatusOffline, nil)

	first := f.post(t, regBody(regTestD1, "renamed-1", nil), regTestRegKey, nil)
	require.Equal(t, http.StatusOK, first.Code)
	k1 := decodeRegResp(t, first).APIKey
	require.NotEmpty(t, k1)

	// A second registration-key holder (or a replay) cannot mint another key.
	f2 := newRegisterFixture(t, false, false) // nil hub: any side effect panics
	f2.dev, f2.h.devices = f.dev, f.dev
	f2.admin, f2.h.adminRepo = f.admin, f.admin
	before := f.state()
	second := f2.post(t, regBody(regTestD1, "renamed-2", nil), regTestRegKey, nil)

	assertReject401(t, second)
	assertUnchanged(t, before, f.state(), "replayed registration-key bootstrap")
	assert.Equal(t, []string{k1}, f.keysOf(regTestD1), "exactly one key; the replay minted none")
	assert.Equal(t, "renamed-1", f.dev.Devices[regTestD1].Hostname, "replay must not rename the device")
}

// Covers AC-012 / FR-009 / PO-1: key for D2 plus a qualifying certificate for the
// NAMED device D1 is still 403 (the wrong-device key wins), end to end.
func TestRegisterDevice_QA_AC003_WrongDeviceKeyWithNamedDeviceCert_403(t *testing.T) {
	f := newRegisterFixture(t, false, true)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil)
	f.seedDevice(regTestD2, "d2-host", models.DeviceStatusOnline, nil)
	f.seedKey("riot_k2", regTestD2)
	before := f.state()

	rec := f.post(t, regBody(regTestD1, "x", nil), "riot_k2", regTestVerifiedTLS(regTestLeaf(regTestD1, 500)))

	assertReject403(t, rec)
	assertUnchanged(t, before, f.state(), "mixed identity: key->D2, cert->D1, naming D1")
}

// Covers AC-008 / FR-005 / V-04: certificate for D with the registration key
// presented, on a device that already has keys. The certificate path mints one
// more key and every prior key stays valid.
func TestRegisterDevice_QA_AC008_CertPlusRegKey_KeyedDevice_AddsKey(t *testing.T) {
	f := newRegisterFixture(t, true, true)
	f.setRegKey(regTestRegKey)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOnline, nil)
	f.seedKey("riot_k1", regTestD1)

	rec := f.post(t, regBody(regTestD1, "renamed", nil), regTestRegKey, regTestVerifiedTLS(regTestLeaf(regTestD1, 501)))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	newKey := decodeRegResp(t, rec).APIKey
	require.NotEmpty(t, newKey)
	assert.ElementsMatch(t, []string{"riot_k1", newKey}, f.keysOf(regTestD1))
}

// Covers AC-011: mTLS inactive (nil caRepo) with a verified chain presented, but
// the caller also has its own valid key: path (a) applies, the cert is ignored,
// and no key is minted.
func TestRegisterDevice_QA_AC011_MTLSDisabled_CertIgnored_OwnKeyStillWorks(t *testing.T) {
	f := newRegisterFixture(t, true, false)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOnline, nil)
	f.seedKey("riot_k1", regTestD1)

	rec := f.post(t, regBody(regTestD1, "renamed", nil), "riot_k1", regTestVerifiedTLS(regTestLeaf(regTestD2, 502)))

	require.Equal(t, http.StatusOK, rec.Code,
		"with mTLS inactive the certificate must be ignored, even one naming another device; body: %s", rec.Body.String())
	assert.Equal(t, "", decodeRegResp(t, rec).APIKey)
	assert.Equal(t, []string{"riot_k1"}, f.keysOf(regTestD1))
}

// Covers AC-014: a rejection for an existing device and for an unknown device are
// indistinguishable on a registration-key server even when the request carries a
// TLS state, a long header, or unusual casing.
func TestRegisterDevice_QA_AC014_ExistingVsUnknown_IdenticalWithTLSStateAndLongKey(t *testing.T) {
	f := newRegisterFixture(t, false, true)
	f.setRegKey(regTestRegKey)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil)
	f.seedKey("riot_k1", regTestD1)
	longKey := strings.Repeat("k", 8192)

	a := f.post(t, regBody(regTestD1, "x", nil), longKey, nil)
	b := f.post(t, regBody(regTestUnknownUUID, "x", nil), longKey, nil)

	assertReject401(t, a)
	assertReject401(t, b)
	assert.Equal(t, a.Body.String(), b.Body.String())
	assert.Equal(t, a.Header(), b.Header())
}

// ---------------------------------------------------------------------------
// Timestamp clamp at the handler level
// ---------------------------------------------------------------------------

// Covers AC-028 (boundary) for both ingest endpoints: exactly R+5m is kept as
// sent, R+5m+1ns is clamped to R.
func TestIngest_QA_AC028_ExactAllowanceBoundary(t *testing.T) {
	kinds := []struct {
		name string
		send func(t *testing.T, h *Handlers, ts time.Time) int
	}{
		{"snapshot",
			func(t *testing.T, h *Handlers, ts time.Time) int {
				return postTelemetry(t, h, "dev-1", snapshotBody(ts)).Code
			}},
		{"heartbeat",
			func(t *testing.T, h *Handlers, ts time.Time) int {
				return postHeartbeat(t, h, "dev-1", heartbeatBody(ts)).Code
			}},
	}
	for _, k := range kinds {
		k := k
		t.Run("[AC-028] "+k.name, func(t *testing.T) {
			clock := clampTestNow
			h, repo := newClampTestHandlers(t, &clock)
			stored := func() []time.Time {
				var out []time.Time
				if k.name == "snapshot" {
					for _, s := range repo.Snapshots["dev-1"] {
						out = append(out, s.Timestamp)
					}
				} else {
					for _, hb := range repo.Heartbeats["dev-1"] {
						out = append(out, hb.Timestamp)
					}
				}
				return out
			}

			require.Equal(t, http.StatusOK, k.send(t, h, clock.Add(5*time.Minute)))
			require.Equal(t, http.StatusOK, k.send(t, h, clock.Add(5*time.Minute+time.Nanosecond)))

			got := stored()
			require.Len(t, got, 2)
			assert.True(t, got[0].Equal(clock.Add(5*time.Minute)), "exactly R+5m must be kept as sent: got %s", got[0])
			assert.True(t, got[1].Equal(clock), "R+5m+1ns must be clamped to R: got %s", got[1])
		})
	}
}

// Covers FR-033 / AC-027 / AC-030: far-past timestamps are stored as sent and a
// missing heartbeat timestamp defaults to the receive time.
func TestIngest_QA_AC027_FarPastKept_ZeroDefaults(t *testing.T) {
	clock := clampTestNow
	h, repo := newClampTestHandlers(t, &clock)
	past := time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)

	require.Equal(t, http.StatusOK, postTelemetry(t, h, "dev-1", snapshotBody(past)).Code)
	require.Equal(t, http.StatusOK, postHeartbeat(t, h, "dev-1", heartbeatBody(past)).Code)
	require.Equal(t, http.StatusOK, postHeartbeat(t, h, "dev-1",
		`{"data":{"uptime":1,"cpu_percent":1,"mem_percent":1,"load_avg_1m":0.1,"disk_root_percent":1}}`).Code)

	require.Len(t, repo.Snapshots["dev-1"], 1)
	assert.True(t, repo.Snapshots["dev-1"][0].Timestamp.Equal(past), "far-past snapshot must be stored as sent")
	require.Len(t, repo.Heartbeats["dev-1"], 2)
	assert.True(t, repo.Heartbeats["dev-1"][0].Timestamp.Equal(past), "far-past heartbeat must be stored as sent")
	assert.True(t, repo.Heartbeats["dev-1"][1].Timestamp.Equal(clock), "missing heartbeat timestamp defaults to receive time")
}

// Covers AC-025 / AC-029 / NFR-007: extreme future values (year 9999) are
// clamped and accepted; the clamp log carries a finite skew and no panic occurs
// from duration saturation.
func TestIngest_QA_AC025_ExtremeFutureClampedWithoutOverflow(t *testing.T) {
	buf := captureLogs(t)
	clock := clampTestNow
	h, repo := newClampTestHandlers(t, &clock)
	far := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

	require.Equal(t, http.StatusOK, postTelemetry(t, h, "dev-1", snapshotBody(far)).Code)
	require.Equal(t, http.StatusOK, postHeartbeat(t, h, "dev-1", heartbeatBody(far)).Code)

	assert.True(t, repo.Snapshots["dev-1"][0].Timestamp.Equal(clock))
	assert.True(t, repo.Heartbeats["dev-1"][0].Timestamp.Equal(clock))
	n := 0
	for _, l := range parseLogLines(t, buf) {
		if l["msg"] == "future-dated timestamp clamped" {
			n++
			assert.Equal(t, "dev-1", l["device_id"])
		}
	}
	assert.Equal(t, 2, n, "one entry per kind")
}

// Covers FR-035 / NFR-009: the debounce is safe and exact under concurrent use.
func TestClampLogLimiter_QA_AC031_ConcurrentObserveEmitsOncePerWindow(t *testing.T) {
	l := newClampLogLimiter()
	const workers, per = 16, 200
	var wg sync.WaitGroup
	var mu sync.Mutex
	emits, total := 0, 0
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				e, n := l.observe("dev-1", clampKindSnapshot, clampTestNow.Add(time.Second))
				if e {
					mu.Lock()
					emits++
					total += n
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, emits, "exactly one entry per device and kind per window, even under concurrency")
	assert.Equal(t, 1, total)
	e, n := l.observe("dev-1", clampKindSnapshot, clampTestNow.Add(2*clampLogWindow))
	assert.True(t, e)
	assert.Equal(t, workers*per, n, "the next entry reports every suppressed clamp, none lost")
}

// Covers AC-024 / AC-002 (Bug C) / AC-007: the REAL released agent HTTP client
// (agent.HTTPClient.Register, unmodified) driven against the REAL RegisterDevice
// handler over HTTP, on a server with a registration key configured.
func TestRegisterDevice_QA_AC024_RealAgentClient_BugC_EndToEnd(t *testing.T) {
	f := newRegisterFixture(t, true, false)
	f.setRegKey(regTestRegKey)
	srv := httptest.NewServer(http.HandlerFunc(f.h.RegisterDevice))
	defer srv.Close()
	ctx := context.Background()

	// 1. First install: the installer wrote the registration key into server.api_key.
	first, err := agent.NewHTTPClient(srv.URL, regTestRegKey).Register(ctx,
		&models.DeviceRegistration{Hostname: "agent-host", Arch: "amd64", AgentVersion: "1.0.0"})
	require.NoError(t, err)
	require.NotEmpty(t, first.APIKey)
	require.NotEmpty(t, first.DeviceID)

	// 2. Periodic re-registration: the agent now holds ONLY its device key (register.go
	// replaced server.api_key). Before this story the registration-key gate rejected it.
	again, err := agent.NewHTTPClient(srv.URL, first.APIKey).Register(ctx,
		&models.DeviceRegistration{Hostname: "agent-host-2", Arch: "amd64", AgentVersion: "1.0.1", DeviceID: first.DeviceID})
	require.NoError(t, err, "Bug C: the agent's own key must be accepted while a registration key is configured")
	assert.Equal(t, first.DeviceID, again.DeviceID)
	assert.Empty(t, again.APIKey, "no new key on own-key re-registration")
	assert.Equal(t, "agent-host-2", f.dev.Devices[first.DeviceID].Hostname)

	// 3. The registration key alone can no longer reclaim a keyed device.
	_, err = agent.NewHTTPClient(srv.URL, regTestRegKey).Register(ctx,
		&models.DeviceRegistration{Hostname: "hijack", DeviceID: first.DeviceID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unauthorized")
	assert.Equal(t, "agent-host-2", f.dev.Devices[first.DeviceID].Hostname, "rejected request must not rename the device")
	assert.Equal(t, []string{first.APIKey}, f.keysOf(first.DeviceID))

	// 4. An agent with an empty key (open-server style request) is rejected too.
	_, err = agent.NewHTTPClient(srv.URL, "").Register(ctx,
		&models.DeviceRegistration{Hostname: "hijack", DeviceID: first.DeviceID})
	require.Error(t, err)
}

package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func ptrInt(v int) *int { return &v }

func TestCL_AC101_NormalizeMapsLegacyRows(t *testing.T) {
	cases := map[string]string{
		"pending": "queued", "sent": "dispatched", "success": "succeeded", "error": "failed", "queued": "queued",
	}
	for stored, want := range cases {
		c := Command{Status: stored}
		c.Normalize()
		if c.Status != want {
			t.Errorf("Normalize on legacy %q: Status = %q, want %q (API must return canonical values)", stored, c.Status, want)
		}
	}
}

func TestCL_AC091_NormalizeDerivesTargetKeyAndDefaults(t *testing.T) {
	c := Command{Status: "queued", TargetKeys: []string{"docker:compose:/a", "host"}}
	c.Normalize()
	if c.TargetKey != "docker:compose:/a" {
		t.Errorf("TargetKey = %q, want the first target key", c.TargetKey)
	}
	if c.Origin != OriginUser {
		t.Errorf("an empty origin must default to user, got %q", c.Origin)
	}
	empty := Command{Status: "queued"}
	empty.Normalize()
	if empty.TargetKeys == nil || len(empty.TargetKeys) != 0 || empty.TargetKey != "" {
		t.Errorf("no keys: TargetKeys=%#v TargetKey=%q, want [] and \"\"", empty.TargetKeys, empty.TargetKey)
	}
	keep := Command{Status: "queued", Origin: OriginAutoPatch}
	keep.Normalize()
	if keep.Origin != OriginAutoPatch {
		t.Errorf("Normalize must not overwrite an origin, got %q", keep.Origin)
	}
}

func TestCL_AC091_CommandJSONShape(t *testing.T) {
	disp := t0.Add(time.Second)
	c := Command{
		ID: "id", DeviceID: "dev", Action: "docker_update", Status: "running",
		ExpiresAt: t0, ExecDeadlineS: 2100, DispatchedAt: &disp,
		Step: "pull", Progress: ptrInt(40), PolicyID: ptrInt(7),
		TargetKeys: []string{"docker:container:web"}, PreLifecycle: true, SendAmbiguous: true,
	}
	c.Normalize()
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "device_id", "action", "params", "status", "result_msg", "created_at", "updated_at",
		"expires_at", "exec_deadline_s", "dispatched_at", "step", "progress", "failure_step", "stderr_tail",
		"origin", "policy_id", "target_key", "target_keys"} {
		if _, ok := m[k]; !ok {
			t.Errorf("Command JSON is missing %q (ADD 6.1)", k)
		}
	}
	for _, k := range []string{"pre_lifecycle", "send_ambiguous", "PreLifecycle", "SendAmbiguous"} {
		if _, ok := m[k]; ok {
			t.Errorf("Command JSON must not expose %q", k)
		}
	}
	for _, k := range []string{"acknowledged_at", "started_at", "verifying_at", "finished_at", "deadline_at"} {
		if _, ok := m[k]; ok {
			t.Errorf("unset timestamp %q must be absent, not null", k)
		}
	}
	if m["progress"] != float64(40) {
		t.Errorf("progress = %v, want 40", m["progress"])
	}
	noProgress, _ := json.Marshal(Command{Status: "queued"})
	if !strings.Contains(string(noProgress), `"progress":null`) {
		t.Errorf("unset progress must serialize as null: %s", noProgress)
	}
}

func TestCL_AC091_CommandJSONRoundTrip(t *testing.T) {
	fin := t0.Add(time.Minute)
	in := Command{ID: "a", Status: "succeeded", FinishedAt: &fin, TargetKeys: []string{"host"}, ExpiresAt: t0}
	raw, _ := json.Marshal(in)
	var out Command
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.FinishedAt == nil || !out.FinishedAt.Equal(fin) || !reflect.DeepEqual(out.TargetKeys, in.TargetKeys) {
		t.Errorf("round trip lost data: %+v", out)
	}
}

func TestCL_AC099_FinalResultLegacyStatusPlusFields(t *testing.T) {
	r := CommandResult{
		CommandID: "c", Status: ResultStatusError, Message: "boom",
		Step: "pull", Outcome: OutcomeFailed, Stderr: "registry said no", Reason: ReasonTimeout,
	}
	raw, _ := json.Marshal(r)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["status"] != "error" {
		t.Errorf("status must stay in the legacy vocabulary, got %v", m["status"])
	}
	for k, want := range map[string]string{"step": "pull", "outcome": "failed", "stderr": "registry said no", "reason": "timeout"} {
		if m[k] != want {
			t.Errorf("result field %q = %v, want %q", k, m[k], want)
		}
	}
	// A legacy agent's result (no new fields) still decodes and resolves.
	var legacy CommandResult
	if err := json.Unmarshal([]byte(`{"command_id":"c","status":"success","message":"ok"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if st, ok := legacy.ResolveState(); st != CommandSucceeded || !ok {
		t.Errorf("legacy success resolves to %q,%v; want succeeded,true", st, ok)
	}
	plain, _ := json.Marshal(CommandResult{CommandID: "c", Status: "success"})
	for _, k := range []string{"step", "outcome", "stderr", "reason"} {
		if strings.Contains(string(plain), `"`+k+`"`) {
			t.Errorf("empty %q must be omitted so legacy servers see the old shape: %s", k, plain)
		}
	}
}

func TestCL_AC097_ResolveState(t *testing.T) {
	cases := []struct {
		name       string
		r          CommandResult
		want       CommandState
		recognized bool
	}{
		{"outcome succeeded wins over status error", CommandResult{Status: "error", Outcome: "succeeded"}, CommandSucceeded, true},
		{"outcome failed wins over status success", CommandResult{Status: "success", Outcome: "failed"}, CommandFailed, true},
		{"legacy success", CommandResult{Status: "success"}, CommandSucceeded, true},
		{"legacy error", CommandResult{Status: "error"}, CommandFailed, true},
		{"unknown outcome falls back to status", CommandResult{Status: "success", Outcome: "weird"}, CommandSucceeded, true},
		{"unknown everything is failed and unrecognized", CommandResult{Status: "maybe"}, CommandFailed, false},
		{"empty is failed and unrecognized", CommandResult{}, CommandFailed, false},
		{"canonical words in status are not accepted", CommandResult{Status: "succeeded"}, CommandFailed, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, ok := tc.r.ResolveState()
			if st != tc.want || ok != tc.recognized {
				t.Errorf("ResolveState = %q,%v; want %q,%v", st, ok, tc.want, tc.recognized)
			}
		})
	}
}

func TestCL_AC107_GuardStates(t *testing.T) {
	claim := CommandTransition{Actor: ActorServer, To: CommandDispatched}
	if got := SQLStatusList(claim.GuardStates()...); got != `'queued','pending'` {
		t.Errorf("claim guard = %s", got)
	}
	revert := CommandTransition{Actor: ActorServer, To: CommandQueued, From: []CommandState{CommandDispatched}}
	if got := SQLStatusList(revert.GuardStates()...); got != `'dispatched','sent'` {
		t.Errorf("revert guard (narrowed to dispatched) = %s, want dispatched+sent alias", got)
	}
	cancel := CommandTransition{Actor: ActorOperator, To: CommandCancelled}
	if got := SQLStatusList(cancel.GuardStates()...); got != `'queued','pending'` {
		t.Errorf("cancel guard = %s", got)
	}
	result := CommandTransition{Actor: ActorAgent, To: CommandFailed}
	if got := result.GuardStates(); !containsState(got, CommandTimedOut) || containsState(got, CommandFailed) {
		t.Errorf("agent result guard must accept timed_out (late result) and never failed: %v", got)
	}
}

func TestCL_AC107_GuardStatesNarrowingCannotWiden(t *testing.T) {
	// A caller-supplied From can only narrow: a source that is not a legal edge
	// for this actor is dropped, never added.
	tr := CommandTransition{Actor: ActorOperator, To: CommandCancelled, From: []CommandState{CommandRunning, CommandQueued}}
	if got := tr.GuardStates(); len(got) != 2 || got[0] != CommandQueued || got[1] != LegacyPending {
		t.Errorf("cancel with From=[running,queued] guard = %v, want [queued pending]", got)
	}
	none := CommandTransition{Actor: ActorOperator, To: CommandCancelled, From: []CommandState{CommandRunning}}
	if got := none.GuardStates(); len(got) != 0 {
		t.Errorf("cancel narrowed to running must have an empty guard, got %v", got)
	}
	bogus := CommandTransition{Actor: ActorAgent, To: CommandSucceeded, From: []CommandState{"x') OR 1=1"}}
	if got := bogus.GuardStates(); len(got) != 0 {
		t.Errorf("an unknown From entry must yield no guard states, got %v", got)
	}
	noActor := CommandTransition{To: CommandSucceeded}
	if got := noActor.GuardStates(); len(got) != 0 {
		t.Errorf("a missing actor must yield no guard states, got %v", got)
	}
}

func TestCL_AC096_NewCommandUpdateShape(t *testing.T) {
	c := Command{
		ID: "cid", DeviceID: "dev", Action: "docker_update", Status: "running", Step: "pull",
		Progress: ptrInt(10), ResultMsg: "pulling", FailureStep: "", Origin: OriginAutoUpdate,
		TargetKeys: []string{"docker:container:web"}, UpdatedAt: t0,
	}
	u := NewCommandUpdate(c)
	raw, _ := json.Marshal(u)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for _, k := range []string{"command_id", "device_id", "action", "target_key", "target_keys", "status", "step",
		"progress", "message", "failure_step", "origin", "updated_at"} {
		if _, ok := m[k]; !ok {
			t.Errorf("command_update is missing %q (ADD 6.5)", k)
		}
	}
	if u.TargetKey != "docker:container:web" || u.Message != "pulling" || u.Status != "running" || u.CommandID != "cid" {
		t.Errorf("unexpected update: %+v", u)
	}
	legacy := NewCommandUpdate(Command{Status: "sent"})
	if legacy.Status != "dispatched" || legacy.TargetKeys == nil {
		t.Errorf("a legacy row must broadcast canonical status and [] keys: %+v", legacy)
	}
}

func TestCL_AC096_ReportConversions(t *testing.T) {
	ack := CommandAck{CommandID: "c", At: t0}.Report()
	if ack.Kind != ReportKindAck || ack.CommandID != "c" {
		t.Errorf("ack report = %+v", ack)
	}
	p := CommandProgress{CommandID: "c", State: "running", Step: "pull", Progress: ptrInt(5), Message: "m"}.Report()
	if p.Kind != ReportKindProgress || p.State != "running" || p.Step != "pull" || *p.Progress != 5 || p.Message != "m" {
		t.Errorf("progress report = %+v", p)
	}
	res := CommandResult{CommandID: "c", Status: "success"}
	rep := res.Report()
	res.Status = "mutated"
	if rep.Kind != ReportKindResult || rep.Result == nil || rep.Result.Status != "success" {
		t.Errorf("result report must carry its own copy of the result: %+v", rep)
	}
}

func TestCL_AC090_ReportBodyDecodes(t *testing.T) {
	body := `{"kind":"progress","command_id":"c","state":"verifying","step":"verify","progress":150,"message":"m"}`
	var r CommandReport
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	if r.Kind != ReportKindProgress || r.State != "verifying" || r.Progress == nil || *r.Progress != 150 {
		t.Errorf("decoded %+v; out-of-range progress must survive decoding so the server can drop it (FR-078)", r)
	}
}

func TestCL_AC096_DispositionAndReceipt(t *testing.T) {
	for d, want := range map[ReportDisposition]bool{
		ReportAccepted: true, ReportDuplicate: true, ReportCoalesced: true,
		ReportAlreadyTerminal: false, ReportInvalidTransition: false, ReportNotFound: false,
	} {
		if d.IsSuccessful() != want {
			t.Errorf("%s.IsSuccessful() = %v, want %v", d, d.IsSuccessful(), !want)
		}
	}
	raw, _ := json.Marshal(CommandReceipt{CommandID: "c", Result: ReportNotFound})
	if string(raw) != `{"command_id":"c","result":"not_found","status":""}` {
		t.Errorf("receipt wire shape = %s", raw)
	}
	ok, _ := json.Marshal(ReportResponse{CommandID: "c", Result: ReportAccepted, Status: "running"})
	if string(ok) != `{"command_id":"c","result":"accepted","status":"running"}` {
		t.Errorf("200 body = %s", ok)
	}
	conflict, _ := json.Marshal(ReportResponse{Error: "x", Code: CodeAlreadyTerminal, Status: "succeeded"})
	if string(conflict) != `{"status":"succeeded","error":"x","code":"already_terminal"}` {
		t.Errorf("409 body = %s", conflict)
	}
}

func TestCL_AC071_ConflictAndBulkResponses(t *testing.T) {
	raw, _ := json.Marshal(CommandConflictResponse{
		Error: ConflictMessage, Code: CodeConflict, ExistingCommandID: "e", ExistingStatus: "running", TargetKey: "host",
	})
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["code"] != "conflict" || m["existing_command_id"] != "e" || m["existing_status"] != "running" || m["target_key"] != "host" {
		t.Errorf("conflict body = %s", raw)
	}
	if _, ok := m["conflicts"]; ok {
		t.Error("a single-target conflict has no conflicts array")
	}

	bulk := BulkCommandResponse{
		Command:   Command{ID: "b", Status: "queued", Action: "docker_bulk_update", TargetKeys: []string{}},
		Conflicts: []BulkConflict{{ContainerID: "c1", TargetKey: "docker:container:web", ExistingCommandID: "e"}},
	}
	raw, _ = json.Marshal(bulk)
	m = map[string]any{}
	_ = json.Unmarshal(raw, &m)
	if m["id"] != "b" || m["action"] != "docker_bulk_update" {
		t.Errorf("bulk 201 must flatten the Command fields: %s", raw)
	}
	cs, ok := m["conflicts"].([]any)
	if !ok || len(cs) != 1 {
		t.Fatalf("bulk 201 must carry the conflicts array: %s", raw)
	}
	first := cs[0].(map[string]any)
	if first["container_id"] != "c1" || first["target_key"] != "docker:container:web" || first["existing_command_id"] != "e" {
		t.Errorf("conflict entry = %v", first)
	}
}

func TestCL_AC009_AutoUpdatePolicyJSON(t *testing.T) {
	failed := t0
	raw, _ := json.Marshal(AutoUpdatePolicy{ID: 1, ConsecutiveFailures: 3, LastFailureAt: &failed})
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["consecutive_failures"] != float64(3) {
		t.Errorf("consecutive_failures = %v, want 3", m["consecutive_failures"])
	}
	if _, ok := m["last_failure_at"]; !ok {
		t.Error("last_failure_at must be present when set")
	}
	raw, _ = json.Marshal(AutoUpdatePolicy{ID: 1})
	if strings.Contains(string(raw), "last_failure_at") {
		t.Errorf("unset last_failure_at must be omitted: %s", raw)
	}
}

func TestCL_AC062_ContainerInfoFreshnessFields(t *testing.T) {
	yes := true
	raw, _ := json.Marshal(ContainerInfo{Name: "web", ImageID: "sha256:abc", UpdateStatus: UpdateStatusAvailable,
		UpdateReason: UpdateReasonPulledNotRecreated, UpdateAvailable: &yes})
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["image_id"] != "sha256:abc" || m["update_status"] != "update_available" || m["update_reason"] != "pulled_not_recreated" {
		t.Errorf("freshness fields = %s", raw)
	}
	bare, _ := json.Marshal(ContainerInfo{Name: "web"})
	for _, k := range []string{"image_id", "update_status", "update_reason"} {
		if strings.Contains(string(bare), k) {
			t.Errorf("empty %s must be omitted so old telemetry JSON is unchanged: %s", k, bare)
		}
	}
	var old ContainerInfo
	if err := json.Unmarshal([]byte(`{"name":"web","update_available":true}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.UpdateStatus != "" || old.UpdateAvailable == nil || !*old.UpdateAvailable {
		t.Errorf("an old stored telemetry blob must still decode: %+v", old)
	}
	for k, want := range map[string]string{
		UpdateStatusUpToDate: "up_to_date", UpdateStatusAvailable: "update_available",
		UpdateStatusPinned: "pinned", UpdateStatusFailed: "check_failed",
		UpdateReasonRegistryNewer: "registry_newer", UpdateReasonPulledNotRecreated: "pulled_not_recreated",
	} {
		if k != want {
			t.Errorf("freshness constant %q drifted from %q", k, want)
		}
	}
}

func TestCL_SEC003_FutureTimestampClampedToReceivedAt(t *testing.T) {
	recv := t0
	cases := []struct {
		name        string
		snap        time.Time
		want        time.Time
		wantClamped bool
	}{
		{"zero uses received_at (FR-070)", time.Time{}, recv, false},
		{"past is kept", recv.Add(-time.Hour), recv.Add(-time.Hour), false},
		{"equal is kept", recv, recv, false},
		{"within tolerance is kept", recv.Add(90 * time.Second), recv.Add(90 * time.Second), false},
		{"exactly at tolerance is kept", recv.Add(TelemetryFutureTolerance), recv.Add(TelemetryFutureTolerance), false},
		{"one nanosecond past tolerance is clamped", recv.Add(TelemetryFutureTolerance + 1), recv, true},
		{"a year ahead is clamped", recv.Add(365 * 24 * time.Hour), recv, true},
		{"year 2099 is clamped", time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), recv, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, clamped := BoundTelemetryTimestamp(tc.snap, recv)
			if !got.Equal(tc.want) || clamped != tc.wantClamped {
				t.Errorf("BoundTelemetryTimestamp = %v,%v; want %v,%v", got, clamped, tc.want, tc.wantClamped)
			}
		})
	}
	// 5m, not the ADD's original 2m: unified with REG-HARDEN-001's
	// FutureSkewAllowance when main was merged, so one tolerance is enforced.
	if TelemetryFutureTolerance != 5*time.Minute {
		t.Errorf("TelemetryFutureTolerance = %v, want 5m (REG-HARDEN-001 FutureSkewAllowance)", TelemetryFutureTolerance)
	}
}

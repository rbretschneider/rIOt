package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func decodeExtraList(t *testing.T, raw string) ExtraUpdaterList {
	t.Helper()
	var u UpdateInfo
	if err := json.Unmarshal([]byte(`{"extra_updaters":`+raw+`}`), &u); err != nil {
		t.Fatalf("decode of extra_updaters %s must never fail, got: %v", raw, err)
	}
	return u.ExtraUpdaters
}

func TestExtraUpdaterList_EU_AC035_Bounds(t *testing.T) {
	t.Run("[AC-035] telemetry from an older agent decodes with an empty list", func(t *testing.T) {
		var u UpdateInfo
		if err := json.Unmarshal([]byte(`{"pending_updates":3}`), &u); err != nil {
			t.Fatalf("older-agent telemetry must decode: %v", err)
		}
		if len(u.ExtraUpdaters) != 0 {
			t.Errorf("ExtraUpdaters = %v, want empty", u.ExtraUpdaters)
		}
	})

	t.Run("[AC-035] 12 entries keep exactly 8 in input order", func(t *testing.T) {
		var parts []string
		for i := 0; i < 12; i++ {
			parts = append(parts, fmt.Sprintf(`{"id":"u%d","status":"up_to_date"}`, i))
		}
		got := decodeExtraList(t, "["+strings.Join(parts, ",")+"]")
		if len(got) != MaxExtraUpdaters {
			t.Fatalf("kept %d entries, want %d", len(got), MaxExtraUpdaters)
		}
		for i, e := range got {
			if want := fmt.Sprintf("u%d", i); e.ID != want {
				t.Errorf("entry %d id = %q, want %q (input order)", i, e.ID, want)
			}
		}
	})

	t.Run("[AC-035] a bogus status is stored as unknown", func(t *testing.T) {
		got := decodeExtraList(t, `[{"id":"dietpi","status":"bogus"}]`)
		if len(got) != 1 || got[0].Status != ExtraStatusUnknown {
			t.Fatalf("got %+v, want one entry with status unknown", got)
		}
	})

	t.Run("[AC-035] a 500-character version is cut to 32", func(t *testing.T) {
		got := decodeExtraList(t, `[{"id":"dietpi","status":"update_available","installed_version":"`+strings.Repeat("9", 500)+`","available_version":"`+strings.Repeat("1", 500)+`"}]`)
		if len(got) != 1 {
			t.Fatalf("got %d entries, want 1", len(got))
		}
		if len(got[0].InstalledVersion) != MaxExtraUpdaterVersion || len(got[0].AvailableVersion) != MaxExtraUpdaterVersion {
			t.Errorf("version lengths = %d and %d, want %d", len(got[0].InstalledVersion), len(got[0].AvailableVersion), MaxExtraUpdaterVersion)
		}
	})

	t.Run("[AC-035] unknown reason and readiness become unknown, empty stays empty", func(t *testing.T) {
		got := decodeExtraList(t, `[{"id":"dietpi","status":"unknown","unknown_reason":"nope","readiness":"nope"},{"id":"snap","status":"up_to_date"}]`)
		if got[0].UnknownReason != "unknown" || got[0].Readiness != "unknown" {
			t.Errorf("reason=%q readiness=%q, want both unknown", got[0].UnknownReason, got[0].Readiness)
		}
		if got[1].UnknownReason != "" || got[1].Readiness != "" {
			t.Errorf("absent reason and readiness must stay empty, got %q %q", got[1].UnknownReason, got[1].Readiness)
		}
	})

	t.Run("[AC-035] bad ids and duplicate ids are dropped", func(t *testing.T) {
		got := decodeExtraList(t, `[{"id":"Bad Id"},{"id":""},{"id":"dietpi","status":"up_to_date"},{"id":"dietpi","status":"unknown"},{"id":"`+strings.Repeat("a", 33)+`"},{"status":"up_to_date"},7,null]`)
		if len(got) != 1 || got[0].ID != "dietpi" || got[0].Status != ExtraStatusUpToDate {
			t.Fatalf("got %+v, want only the first dietpi entry", got)
		}
	})

	t.Run("[AC-035] notices keep known codes only, at most 8, with bounded values", func(t *testing.T) {
		var notices []string
		for i := 0; i < 12; i++ {
			notices = append(notices, `{"code":"major_upgrade"}`)
		}
		notices = append(notices, `{"code":"bogus"}`)
		got := decodeExtraList(t, `[{"id":"dietpi","notices":[`+strings.Join(notices, ",")+`]}]`)
		if len(got[0].Notices) != MaxExtraUpdaterNotices {
			t.Errorf("notices = %d, want %d", len(got[0].Notices), MaxExtraUpdaterNotices)
		}
		got = decodeExtraList(t, `[{"id":"dietpi","notices":[{"code":"bogus"},{"code":"non_default_update_source","branch":"`+strings.Repeat("b", 200)+`","owner":"x"}]}]`)
		if len(got[0].Notices) != 1 || len(got[0].Notices[0].Branch) != MaxExtraUpdaterString {
			t.Errorf("notices = %+v, want one notice with a 64-byte branch", got[0].Notices)
		}
	})

	t.Run("[AC-035] pending count is clamped and items are bounded", func(t *testing.T) {
		var items []string
		for i := 0; i < 80; i++ {
			items = append(items, `"`+strings.Repeat("n", 100)+`"`)
		}
		got := decodeExtraList(t, `[{"id":"snap","pending_count":999999,"pending_items":[`+strings.Join(items, ",")+`]},{"id":"flatpak","pending_count":-4}]`)
		if got[0].PendingCount != MaxExtraUpdaterPending {
			t.Errorf("pending count = %d, want clamp %d", got[0].PendingCount, MaxExtraUpdaterPending)
		}
		if len(got[0].PendingItems) != MaxExtraUpdaterItems || len(got[0].PendingItems[0]) != MaxExtraUpdaterString {
			t.Errorf("items = %d of %d bytes, want %d of %d", len(got[0].PendingItems), len(got[0].PendingItems[0]), MaxExtraUpdaterItems, MaxExtraUpdaterString)
		}
		if got[1].PendingCount != 0 {
			t.Errorf("negative pending count = %d, want 0", got[1].PendingCount)
		}
	})

	t.Run("[AC-035] a non-array and null decode to nothing without an error", func(t *testing.T) {
		for _, raw := range []string{`null`, `{"id":"dietpi"}`, `"x"`, `5`, `true`, `[1,2]`} {
			if got := decodeExtraList(t, raw); len(got) != 0 {
				t.Errorf("payload %s decoded to %+v, want nothing", raw, got)
			}
		}
	})

	t.Run("[AC-035] a round trip keeps the fields and omits an empty list", func(t *testing.T) {
		b, err := json.Marshal(UpdateInfo{})
		if err != nil || strings.Contains(string(b), "extra_updaters") {
			t.Fatalf("empty list must be omitted, got %s (%v)", b, err)
		}
		in := UpdateInfo{ExtraUpdaters: ExtraUpdaterList{{ID: "dietpi", Status: ExtraStatusUpdateAvailable, InstalledVersion: "9.20.1", AvailableVersion: "10.8.2", PendingCount: 1, Readiness: ExtraReadinessEnabled, Notices: []ExtraUpdaterNotice{{Code: ExtraNoticeMajorUpgrade}}}}}
		b, err = json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var out UpdateInfo
		if err = json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		if len(out.ExtraUpdaters) != 1 || out.ExtraUpdaters[0].AvailableVersion != "10.8.2" || !out.ExtraUpdaters[0].HasNotice(ExtraNoticeMajorUpgrade) || !out.ExtraUpdaters[0].Enabled() || !out.ExtraUpdaters[0].Pending() {
			t.Errorf("round trip lost data: %+v", out.ExtraUpdaters)
		}
	})
}

func TestBounds_EU_AC025_StrictlyNested(t *testing.T) {
	t.Run("[AC-025] step limit < worker budget < service backstop < agent hard max < command deadline", func(t *testing.T) {
		const (
			stepLimit     = 40 * time.Minute
			workerBudget  = 80 * time.Minute
			serviceBackup = 85 * time.Minute
		)
		chain := []time.Duration{stepLimit, workerBudget, serviceBackup, OSUpdateExtraAgentHardMax, OSUpdateExtraExecDeadline}
		for i := 1; i < len(chain); i++ {
			if chain[i-1] >= chain[i] {
				t.Errorf("bound %d (%s) must be below bound %d (%s)", i-1, chain[i-1], i, chain[i])
			}
		}
	})

	t.Run("[AC-025] the deadline invariant holds for the extended tier", func(t *testing.T) {
		if OSUpdateExtraAgentHardMax+DeadlineSlack > OSUpdateExtraExecDeadline {
			t.Errorf("agent hard max %s + slack %s exceeds deadline %s", OSUpdateExtraAgentHardMax, DeadlineSlack, OSUpdateExtraExecDeadline)
		}
	})

	t.Run("[AC-025] the default tier keeps its pre-story values", func(t *testing.T) {
		if got := AgentHardMax(ActionOSUpdate, 0); got != 31*time.Minute+30*time.Second {
			t.Errorf("default os_update agent hard max = %s, want 31m30s", got)
		}
		if got := ExecDeadline(ActionOSUpdate, 0); got != 40*time.Minute {
			t.Errorf("default os_update deadline = %s, want 40m", got)
		}
	})

	t.Run("[AC-025] only a boolean true budget stamp extends the bounds", func(t *testing.T) {
		cases := []struct {
			name   string
			action string
			params map[string]interface{}
			want   bool
		}{
			{"os_update stamped", ActionOSUpdate, map[string]interface{}{ParamExtraUpdaterBudget: true}, true},
			{"os_update false", ActionOSUpdate, map[string]interface{}{ParamExtraUpdaterBudget: false}, false},
			{"os_update string true", ActionOSUpdate, map[string]interface{}{ParamExtraUpdaterBudget: "true"}, false},
			{"os_update absent", ActionOSUpdate, map[string]interface{}{}, false},
			{"nil params", ActionOSUpdate, nil, false},
			{"other action stamped", ActionReboot, map[string]interface{}{ParamExtraUpdaterBudget: true}, false},
		}
		for _, tc := range cases {
			if got := ExtendedOSUpdate(tc.action, tc.params); got != tc.want {
				t.Errorf("%s: ExtendedOSUpdate = %v, want %v", tc.name, got, tc.want)
			}
		}
	})
}

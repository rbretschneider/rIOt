package models

import (
	"encoding/json"
	"testing"
)

func decodeUpdateInfo(t *testing.T, raw string) UpdateInfo {
	t.Helper()
	var u UpdateInfo
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("decode of %s must never fail, got: %v", raw, err)
	}
	return u
}

func TestUpdateInfo_PF_AC029_FreshnessLenientDecode(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantRaw  ListsFreshness
		wantNorm string
	}{
		{"absent (older agent)", `{}`, "", "unknown"},
		{"null", `{"lists_freshness":null}`, "", "unknown"},
		{"fresh", `{"lists_freshness":"fresh"}`, ListsFresh, "fresh"},
		{"stale", `{"lists_freshness":"stale"}`, ListsStale, "stale"},
		{"unknown", `{"lists_freshness":"unknown"}`, ListsUnknown, "unknown"},
		{"bogus string", `{"lists_freshness":"bogus"}`, ListsUnknown, "unknown"},
		{"wrong case", `{"lists_freshness":"STALE"}`, ListsUnknown, "unknown"},
		{"number", `{"lists_freshness":42}`, ListsUnknown, "unknown"},
		{"bool", `{"lists_freshness":true}`, ListsUnknown, "unknown"},
		{"object", `{"lists_freshness":{"a":1}}`, ListsUnknown, "unknown"},
		{"array", `{"lists_freshness":["stale"]}`, ListsUnknown, "unknown"},
	}
	for _, tc := range cases {
		t.Run("[AC-029] "+tc.name, func(t *testing.T) {
			u := decodeUpdateInfo(t, tc.raw)
			if u.ListsFreshness != tc.wantRaw {
				t.Errorf("stored freshness = %q, want %q", u.ListsFreshness, tc.wantRaw)
			}
			if got := u.Freshness(); got != tc.wantNorm {
				t.Errorf("Freshness() = %q, want %q", got, tc.wantNorm)
			}
		})
	}

	t.Run("[AC-029] nil UpdateInfo reads as unknown", func(t *testing.T) {
		var u *UpdateInfo
		if got := u.Freshness(); got != "unknown" {
			t.Errorf("nil Freshness() = %q, want unknown", got)
		}
	})
}

func TestUpdateInfo_PF_AC029_FreshnessRoundTripOmitsEmpty(t *testing.T) {
	b, err := json.Marshal(UpdateInfo{})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["lists_freshness"]; ok {
		t.Error("empty freshness must be omitted so older servers see an unchanged payload")
	}
	if _, ok := m["supports_reboot_if_required"]; ok {
		t.Error("false capability must be omitted so older servers see an unchanged payload")
	}
}

func TestUpdateInfo_PF_AC016_CapabilityOnlyTrueCounts(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"absent", `{}`, false},
		{"true", `{"supports_reboot_if_required":true}`, true},
		{"false", `{"supports_reboot_if_required":false}`, false},
		{"null", `{"supports_reboot_if_required":null}`, false},
		{"string true", `{"supports_reboot_if_required":"true"}`, false},
		{"number one", `{"supports_reboot_if_required":1}`, false},
		{"object", `{"supports_reboot_if_required":{}}`, false},
		{"array", `{"supports_reboot_if_required":[true]}`, false},
	}
	for _, tc := range cases {
		t.Run("[AC-016] "+tc.name, func(t *testing.T) {
			u := decodeUpdateInfo(t, tc.raw)
			if bool(u.SupportsRebootIfRequired) != tc.want {
				t.Errorf("capability = %v, want %v", u.SupportsRebootIfRequired, tc.want)
			}
		})
	}
}

package maintenance

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// TestWorker_DR_AC002_V1PostRefreshRecordPinned pins what a version 1 run
// records when the apt configuration changes during the upgrade, so that
// DIETPI-REFRESH-001 provably leaves version 1 untouched (FR-001, NFR-005).
//
// It was written and committed against the code as it stood before the story
// changed any production file. Do not regenerate the expected bytes from newer
// code: a difference is a regression of version 1.
func TestWorker_DR_AC002_V1PostRefreshRecordPinned(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC) }
	const head = `{"version":1,"command_id":"3db2d182-61f4-4a4a-b515-243000a43d34",`
	const times = `"started_at":"2026-10-27T03:00:00Z","updated_at":"2026-10-27T03:00:00Z","finished_at":"2026-10-27T03:00:00Z",`
	const okTail = `"phase":"finished","outcome":"success","exit_code":0,"interrupted":false,`
	const okSummary = `"summary":"Package state verified; maintenance completed",`
	const apt = "\n[configure]\n\n[audit]\n\n[check]\n\n[refresh]\n\n[upgrade]\n\n[audit]\n\n[check]\n"
	const plainTail = `"output_truncated":false,"holds":{},"holds_released":false,"hold_cleanup_failed":false}`
	const rebootTail = `"output_truncated":false,"holds":{"packages":["linux-image-generic"],"versions":{"linux-image-generic":"1"}},"holds_released":false,"hold_cleanup_failed":false,"version_changes":[{"package":"linux-image-generic","before":"1","after":"2"}]}`
	outputJSON := func(s string) string { b, _ := json.Marshal(s); return `"output":` + string(b) + `,` }

	cases := []struct {
		name        string
		reboot      bool
		postFail    string
		wantJSON    string
		wantPhases  []string
		wantStamps  int
		wantRefresh int
	}{
		{
			name:        "plain",
			wantJSON:    head + `"mode":"full","include_reboot_class":false,` + times + okTail + okSummary + outputJSON(apt+"\n[post_refresh]\nHit:1 deb.debian.org") + plainTail,
			wantPhases:  []string{"accepted", "configure", "audit", "check", "refresh", "upgrade", "audit", "check", "post_refresh", "finished"},
			wantStamps:  2,
			wantRefresh: 7,
		},
		{
			name:        "with reboot class",
			reboot:      true,
			wantJSON:    head + `"mode":"full","include_reboot_class":true,` + times + okTail + okSummary + outputJSON(apt+"\n[post_refresh]\nHit:1 deb.debian.org") + rebootTail,
			wantPhases:  []string{"accepted", "hold_release", "configure", "audit", "check", "refresh", "upgrade", "audit", "check", "post_refresh", "hold_restore", "finished"},
			wantStamps:  2,
			wantRefresh: 7,
		},
		{
			name:        "post-run refresh fails",
			postFail:    "boom",
			wantJSON:    head + `"mode":"full","include_reboot_class":false,` + times + okTail + `"summary":"Package state verified; maintenance completed; post-run package list refresh failed: boom",` + outputJSON(apt+"\n[post_refresh]\nboom") + plainTail,
			wantPhases:  []string{"accepted", "configure", "audit", "check", "refresh", "upgrade", "audit", "check", "post_refresh", "finished"},
			wantStamps:  1,
			wantRefresh: 7,
		},
	}
	for _, tc := range cases {
		t.Run("[AC-002] v1 "+tc.name+" record is pinned", func(t *testing.T) {
			w, runner, store := newWorker()
			w.Now = clock
			w.Sources, _ = sourcesSequence("before-run", "after-upgrade")
			touch, stamps := countingTouch()
			w.TouchLists = touch
			runner.outputs = map[int]string{callPostRefresh: "Hit:1 deb.debian.org"}
			if tc.postFail != "" {
				runner.failAt = map[int]string{callPostRefresh: tc.postFail}
			}
			if tc.reboot {
				w.Holds = &fakeHolds{store: store}
			}
			req := testRequest()
			req.IncludeRebootClass = tc.reboot
			rec, err := w.Execute(context.Background(), req)
			if err != nil {
				t.Fatalf("[AC-002] run failed: %v", err)
			}
			got, _ := json.Marshal(rec)
			if string(got) != tc.wantJSON {
				t.Errorf("[AC-002] record bytes changed\n got: %s\nwant: %s", got, tc.wantJSON)
			}
			var phases []string
			for _, r := range store.records {
				phases = append(phases, r.Phase)
			}
			if !reflect.DeepEqual(phases, tc.wantPhases) {
				t.Errorf("[AC-002] phases = %v, want %v", phases, tc.wantPhases)
			}
			if len(runner.calls) != callsWithExtra {
				t.Errorf("[AC-002] runner calls = %d, want %d", len(runner.calls), callsWithExtra)
			}
			if !isRefresh(runner.calls[tc.wantRefresh]) {
				t.Errorf("[AC-002] call %d = %v, want the post-run refresh", tc.wantRefresh, runner.calls[tc.wantRefresh])
			}
			if *stamps != tc.wantStamps {
				t.Errorf("[AC-002] lists stamped %d times, want %d", *stamps, tc.wantStamps)
			}
		})
	}
}

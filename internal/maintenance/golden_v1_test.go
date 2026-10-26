package maintenance

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// Golden bytes. They were produced by running these scenarios against
// internal/maintenance at 410deb0, before EXTRA-UPD-001 touched Execute. A v1
// request must still produce exactly these records (FR-066, AC-044). Do not
// regenerate them from newer code.
func TestExecute_EU_AC044_V1Golden(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC) }
	const head = `{"version":1,"command_id":"3db2d182-61f4-4a4a-b515-243000a43d34",`
	const times = `"started_at":"2026-10-26T03:00:00Z","updated_at":"2026-10-26T03:00:00Z","finished_at":"2026-10-26T03:00:00Z",`
	const okTail = `"phase":"finished","outcome":"success","exit_code":0,"interrupted":false,"summary":"Package state verified; maintenance completed",`
	steps := "\n[configure]\n\n[audit]\n\n[check]\n\n[refresh]\n\n[upgrade]\n\n[audit]\n\n[check]\n"
	tail := `"output_truncated":false,"holds":{},"holds_released":false,"hold_cleanup_failed":false}`
	outputJSON := func(s string) string { b, _ := json.Marshal(s); return `"output":` + string(b) + `,` }

	cases := []struct {
		name       string
		mode       string
		reboot     bool
		failAt     map[int]string
		wantJSON   string
		wantPhases []string
		wantCalls  int
		wantErr    bool
	}{
		{
			name:       "full success",
			mode:       "full",
			wantJSON:   head + `"mode":"full","include_reboot_class":false,` + times + okTail + outputJSON(steps) + tail,
			wantPhases: []string{"accepted", "configure", "audit", "check", "refresh", "upgrade", "audit", "check", "finished"},
			wantCalls:  7,
		},
		{
			name:       "security success",
			mode:       "security",
			wantJSON:   head + `"mode":"security","include_reboot_class":false,` + times + okTail + outputJSON(steps) + tail,
			wantPhases: []string{"accepted", "configure", "audit", "check", "refresh", "upgrade", "audit", "check", "finished"},
			wantCalls:  7,
		},
		{
			name:       "repair success",
			mode:       "repair",
			wantJSON:   head + `"mode":"repair","include_reboot_class":false,` + times + okTail + outputJSON("\n[configure]\n\n[audit]\n\n[check]\n") + tail,
			wantPhases: []string{"accepted", "configure", "audit", "check", "finished"},
			wantCalls:  3,
		},
		{
			name:       "upgrade failure",
			mode:       "full",
			failAt:     map[int]string{4: "E: Unable to correct problems"},
			wantJSON:   head + `"mode":"full","include_reboot_class":false,` + times + `"phase":"finished","outcome":"error","exit_code":100,"interrupted":false,"summary":"upgrade failed: E: Unable to correct problems",` + outputJSON("\n[configure]\n\n[audit]\n\n[check]\n\n[refresh]\n\n[upgrade]\nE: Unable to correct problems") + tail,
			wantPhases: []string{"accepted", "configure", "audit", "check", "refresh", "upgrade", "finished"},
			wantCalls:  5,
			wantErr:    true,
		},
		{
			name:       "reboot-class success",
			mode:       "full",
			reboot:     true,
			wantJSON:   head + `"mode":"full","include_reboot_class":true,` + times + okTail + outputJSON(steps) + `"output_truncated":false,"holds":{"packages":["linux-image-generic"],"versions":{"linux-image-generic":"1"}},"holds_released":false,"hold_cleanup_failed":false,"version_changes":[{"package":"linux-image-generic","before":"1","after":"2"}]}`,
			wantPhases: []string{"accepted", "hold_release", "configure", "audit", "check", "refresh", "upgrade", "audit", "check", "hold_restore", "finished"},
			wantCalls:  7,
		},
	}
	for _, tc := range cases {
		t.Run("[AC-044] v1 "+tc.name+" record is byte-identical to the pre-story golden", func(t *testing.T) {
			w, runner, store := newWorker()
			w.Now = clock
			runner.failAt = tc.failAt
			if tc.reboot {
				w.Holds = &fakeHolds{store: store}
			}
			req := testRequest()
			req.Mode = tc.mode
			req.IncludeRebootClass = tc.reboot
			rec, err := w.Execute(context.Background(), req)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			got, _ := json.Marshal(rec)
			if string(got) != tc.wantJSON {
				t.Errorf("record bytes changed\n got: %s\nwant: %s", got, tc.wantJSON)
			}
			var phases []string
			for _, r := range store.records {
				phases = append(phases, r.Phase)
			}
			if !reflect.DeepEqual(phases, tc.wantPhases) {
				t.Errorf("phases = %v, want %v", phases, tc.wantPhases)
			}
			if len(runner.calls) != tc.wantCalls {
				t.Errorf("runner calls = %d, want %d: %v", len(runner.calls), tc.wantCalls, phases)
			}
		})
	}
	t.Run("[AC-044] a v1 request marshals without any new key", func(t *testing.T) {
		got, _ := json.Marshal(testRequest())
		const want = `{"version":1,"command_id":"3db2d182-61f4-4a4a-b515-243000a43d34","mode":"full","include_reboot_class":false}`
		if string(got) != want {
			t.Errorf("v1 request bytes = %s, want %s", got, want)
		}
	})
}

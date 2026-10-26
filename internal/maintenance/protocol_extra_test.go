package maintenance

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
)

const validV2 = `{"version":2,"command_id":"3db2d182-61f4-4a4a-b515-243000a43d34","mode":"full","include_reboot_class":false,"extra_updaters":["dietpi"]}`

func TestParseRequest_EU_AC019_NoArgvField(t *testing.T) {
	t.Run("[AC-019] a v2 request with one registered updater parses", func(t *testing.T) {
		r, err := ParseRequest([]byte(validV2))
		if err != nil || r.Version != 2 || !reflect.DeepEqual(r.ExtraUpdaters, []string{"dietpi"}) {
			t.Fatalf("%+v %v", r, err)
		}
	})
	t.Run("[AC-019] the request type can carry no argument, environment or path", func(t *testing.T) {
		typ := reflect.TypeOf(Request{})
		var names []string
		for i := 0; i < typ.NumField(); i++ {
			names = append(names, typ.Field(i).Name)
		}
		want := "Version,CommandID,Mode,IncludeRebootClass,ExtraUpdaters"
		if strings.Join(names, ",") != want {
			t.Errorf("Request fields = %v, want %s", names, want)
		}
		if typ.Field(4).Type.Elem().Kind() != reflect.String {
			t.Error("ExtraUpdaters must be a list of identifiers")
		}
	})
	for _, key := range []string{"argv", "args", "env", "path", "command", "dir", "cwd", "stdin"} {
		t.Run("[AC-019] rejects a v2 request carrying "+key, func(t *testing.T) {
			text := strings.Replace(validV2, `"version":2`, `"version":2,"`+key+`":["--evil"]`, 1)
			if _, err := ParseRequest([]byte(text)); err == nil {
				t.Fatalf("a request key %q was accepted", key)
			}
		})
	}
	for _, id := range []string{"/boot/dietpi/dietpi-update", "dietpi-update 1", "dietpi --evil", "DIETPI", "snap", ""} {
		t.Run("[AC-019] rejects the identifier "+id, func(t *testing.T) {
			text := strings.Replace(validV2, `"dietpi"`, `"`+id+`"`, 1)
			if _, err := ParseRequest([]byte(text)); err == nil {
				t.Fatalf("identifier %q was accepted", id)
			}
		})
	}
}

func TestParseRequest_EU_AC044_V1AndV2StayDistinct(t *testing.T) {
	cases := map[string]string{
		"v2 without the list":      `{"version":2,"command_id":"3db2d182-61f4-4a4a-b515-243000a43d34","mode":"full","include_reboot_class":false}`,
		"v1 with the list":         strings.Replace(validV2, `"version":2`, `"version":1`, 1),
		"v2 empty list":            strings.Replace(validV2, `["dietpi"]`, `[]`, 1),
		"v2 null list":             strings.Replace(validV2, `["dietpi"]`, `null`, 1),
		"v2 scalar list":           strings.Replace(validV2, `["dietpi"]`, `"dietpi"`, 1),
		"v2 repair":                strings.Replace(validV2, `"full"`, `"repair"`, 1),
		"v2 duplicate id":          strings.Replace(validV2, `["dietpi"]`, `["dietpi","dietpi"]`, 1),
		"v2 nine ids":              strings.Replace(validV2, `["dietpi"]`, `["dietpi","a","b","c","d","e","f","g","h"]`, 1),
		"v2 duplicate list key":    strings.Replace(validV2, `"extra_updaters":["dietpi"]`, `"extra_updaters":["dietpi"],"extra_updaters":["dietpi"]`, 1),
		"version 3":                strings.Replace(validV2, `"version":2`, `"version":3`, 1),
		"v2 trailing JSON":         validV2 + ` {}`,
		"v2 reboot class + repair": strings.Replace(strings.Replace(validV2, `"full"`, `"repair"`, 1), `false`, `true`, 1),
	}
	for name, text := range cases {
		t.Run("[AC-044] rejects "+name, func(t *testing.T) {
			if _, err := ParseRequest([]byte(text)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	t.Run("[AC-044] security mode and reboot class are valid in v2", func(t *testing.T) {
		text := strings.Replace(strings.Replace(validV2, `"full"`, `"security"`, 1), `false`, `true`, 1)
		if _, err := ParseRequest([]byte(text)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("[AC-044] a v1 request marshals to exactly four keys", func(t *testing.T) {
		data, _ := json.Marshal(testRequest())
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if len(m) != 4 {
			t.Fatalf("v1 request has %d keys: %s", len(m), data)
		}
	})
	t.Run("[AC-044] the v2 request round trips", func(t *testing.T) {
		want := Request{Version: 2, CommandID: testRequest().CommandID, Mode: "security", ExtraUpdaters: []string{"dietpi"}}
		data, _ := json.Marshal(want)
		got, err := ParseRequest(data)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("[AC-044] ProtocolVersion is still 1", func(t *testing.T) {
		if ProtocolVersion != 1 {
			t.Fatalf("ProtocolVersion = %d: it is shared with update.json and --maintenance-protocol (AD-002)", ProtocolVersion)
		}
	})
}

func TestBounds_EU_AC025_WorkerSide(t *testing.T) {
	spec := extraupd.NewDietPi().Apply()
	t.Run("[AC-025] step limit < worker budget < service backstop", func(t *testing.T) {
		chain := []time.Duration{spec.StepLimit, ExtendedWorkerBudget, ExtraServiceBackstop}
		for i := 1; i < len(chain); i++ {
			if chain[i-1] >= chain[i] {
				t.Errorf("%s must be below %s", chain[i-1], chain[i])
			}
		}
	})
	t.Run("[AC-025] the backstop kills after TimeoutStopSec, still inside the agent hard max", func(t *testing.T) {
		if ExtraServiceBackstop+TerminationGrace >= 88*time.Minute {
			t.Errorf("backstop %s + stop %s must stay below the 88m agent hard max", ExtraServiceBackstop, TerminationGrace)
		}
	})
	t.Run("[AC-025] the start threshold covers step, grace, lock wait and a minute", func(t *testing.T) {
		if want := spec.StepLimit + TerminationGrace + ExtraLockWait + time.Minute; ExtraStartThreshold != want {
			t.Errorf("threshold = %s, want %s", ExtraStartThreshold, want)
		}
	})
	t.Run("[AC-025] the worst case still ends inside the extended budget", func(t *testing.T) {
		worst := WorkerBudget + CleanupBudget + ExtraLockWait + ExtraLauncherSetupBound + spec.StepLimit + TerminationGrace + 5*time.Second
		if worst >= ExtendedWorkerBudget {
			t.Errorf("worst case %s must end before %s", worst, ExtendedWorkerBudget)
		}
	})
	t.Run("[AC-025] the minimum accepted backstop sits between the budget and the installed value", func(t *testing.T) {
		if !(ExtendedWorkerBudget < ExtraMinBackstop && ExtraMinBackstop < ExtraServiceBackstop) {
			t.Errorf("min backstop %s must be inside (%s, %s)", ExtraMinBackstop, ExtendedWorkerBudget, ExtraServiceBackstop)
		}
	})
	t.Run("[AC-025] the default tier keeps its pre-story values", func(t *testing.T) {
		if WorkerBudget != 30*time.Minute || CleanupBudget != 2*time.Minute || TerminationGrace != 90*time.Second {
			t.Errorf("default bounds changed: %s %s %s", WorkerBudget, CleanupBudget, TerminationGrace)
		}
	})
}

func sampleV2Record() Record {
	now := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)
	fin := now.Add(time.Hour)
	zero := 0
	return Record{
		Version: 2, CommandID: testRequest().CommandID, Mode: "full", StartedAt: now, UpdatedAt: fin, FinishedAt: &fin,
		Phase: "finished", Outcome: "success", Summary: "ok", RequestedExtraUpdaters: []string{"dietpi"}, AptOutcome: "success",
		ExtraUpdaters: []UpdaterResult{{ID: "dietpi", Outcome: UpdaterSuccess, ExitCode: &zero, StartedAt: &now, FinishedAt: &fin, VersionBefore: "9.20.1", VersionAfter: "10.8.2"}},
	}
}

func TestRecord_EU_AC028_V2Validation(t *testing.T) {
	t.Run("[AC-028] a consistent v2 success record validates and round trips strictly", func(t *testing.T) {
		rec := sampleV2Record()
		data, _ := json.Marshal(rec)
		back, err := parseRecord(data)
		if err != nil || !reflect.DeepEqual(back.ExtraUpdaters, rec.ExtraUpdaters) {
			t.Fatalf("%+v %v", back, err)
		}
	})
	mutate := map[string]func(*Record){
		"success with a failed updater":    func(r *Record) { r.ExtraUpdaters[0].Outcome = UpdaterFailed },
		"success with a timed out updater": func(r *Record) { r.ExtraUpdaters[0].Outcome = UpdaterTimedOut },
		"success with dpkg_locked": func(r *Record) {
			r.ExtraUpdaters[0].Outcome, r.ExtraUpdaters[0].Reason = UpdaterSkipped, SkipDpkgLocked
		},
		"success with insufficient_time": func(r *Record) {
			r.ExtraUpdaters[0].Outcome, r.ExtraUpdaters[0].Reason = UpdaterSkipped, SkipInsufficientTime
		},
		"success after a failed apt part":    func(r *Record) { r.AptOutcome = "error" },
		"terminal without an apt outcome":    func(r *Record) { r.AptOutcome = "" },
		"unknown apt outcome":                func(r *Record) { r.AptOutcome = "partial" },
		"unknown updater outcome":            func(r *Record) { r.ExtraUpdaters[0].Outcome = "partial" },
		"skip without a reason":              func(r *Record) { r.ExtraUpdaters[0].Outcome = UpdaterSkipped },
		"skip with an unknown reason":        func(r *Record) { r.ExtraUpdaters[0].Outcome, r.ExtraUpdaters[0].Reason = UpdaterSkipped, "because" },
		"reason on a non-skip":               func(r *Record) { r.ExtraUpdaters[0].Reason = SkipNotEnabled },
		"running entry in a terminal record": func(r *Record) { r.ExtraUpdaters[0].Outcome = UpdaterRunning },
		"duplicate updater id":               func(r *Record) { r.ExtraUpdaters = append(r.ExtraUpdaters, r.ExtraUpdaters[0]) },
		"bad updater id":                     func(r *Record) { r.ExtraUpdaters[0].ID = "Bad Id" },
		"note over 256 bytes":                func(r *Record) { r.ExtraUpdaters[0].Note = strings.Repeat("n", 257) },
		"version over 32 bytes":              func(r *Record) { r.ExtraUpdaters[0].VersionAfter = strings.Repeat("9", 33) },
		"nine results":                       func(r *Record) { r.ExtraUpdaters = make([]UpdaterResult, 9) },
		"v1 record carrying results":         func(r *Record) { r.Version, r.RequestedExtraUpdaters = 1, nil },
		"requested list unknown":             func(r *Record) { r.RequestedExtraUpdaters = []string{"snap"} },
		"repair with requested updaters":     func(r *Record) { r.Mode = "repair" },
	}
	for name, fn := range mutate {
		t.Run("[AC-028] rejects "+name, func(t *testing.T) {
			rec := sampleV2Record()
			fn(&rec)
			if err := rec.Validate(); err == nil {
				t.Fatalf("accepted: %+v", rec)
			}
		})
	}
	t.Run("[AC-028] neutral skips keep a success record valid", func(t *testing.T) {
		for _, reason := range []string{SkipNotEnabled, SkipStatusUnknown, SkipNothingPending} {
			rec := sampleV2Record()
			rec.ExtraUpdaters = []UpdaterResult{{ID: "dietpi", Outcome: UpdaterSkipped, Reason: reason}}
			if err := rec.Validate(); err != nil {
				t.Errorf("%s: %v", reason, err)
			}
		}
	})
	t.Run("[AC-028] an apt failure with the updater skipped for it is a valid error record", func(t *testing.T) {
		rec := sampleV2Record()
		rec.Outcome, rec.ExitCode, rec.AptOutcome = "error", 100, "error"
		rec.ExtraUpdaters = []UpdaterResult{{ID: "dietpi", Outcome: UpdaterSkipped, Reason: SkipAptNotSuccessful}}
		if err := rec.Validate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("[AC-029] a running entry is valid only in its own running phase", func(t *testing.T) {
		rec := sampleV2Record()
		rec.Outcome, rec.FinishedAt, rec.AptOutcome = "running", nil, "success"
		rec.Phase = "extra_run:dietpi"
		rec.ExtraUpdaters = []UpdaterResult{{ID: "dietpi", Outcome: UpdaterRunning}}
		if err := rec.Validate(); err != nil {
			t.Fatalf("own phase: %v", err)
		}
		rec.Phase = "upgrade"
		if err := rec.Validate(); err == nil {
			t.Fatal("a running entry outside its phase must be rejected")
		}
	})
	t.Run("[AC-044] unknown or duplicate record keys in v2 fields are rejected", func(t *testing.T) {
		data, _ := json.Marshal(sampleV2Record())
		bad := strings.Replace(string(data), `"id":"dietpi"`, `"id":"dietpi","argv":["x"]`, 1)
		if _, err := parseRecord([]byte(bad)); err == nil {
			t.Error("unknown nested field accepted")
		}
		dup := strings.Replace(string(data), `"apt_outcome":"success"`, `"apt_outcome":"success","apt_outcome":"error"`, 1)
		if _, err := parseRecord([]byte(dup)); err == nil {
			t.Error("duplicate field accepted")
		}
	})
	t.Run("[AC-044] a v1 record never serialises a v2 key", func(t *testing.T) {
		now := time.Now().UTC()
		fin := now
		rec := Record{Version: 1, CommandID: testRequest().CommandID, Mode: "full", StartedAt: now, UpdatedAt: now, FinishedAt: &fin, Phase: "finished", Outcome: "success"}
		data, _ := json.Marshal(rec)
		for _, key := range []string{"requested_extra_updaters", "apt_outcome", "extra_updaters"} {
			if strings.Contains(string(data), key) {
				t.Errorf("v1 record JSON contains %s", key)
			}
		}
	})
}

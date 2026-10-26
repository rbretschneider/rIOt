package maintenance

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testRequest() Request {
	return Request{Version: 1, CommandID: "3db2d182-61f4-4a4a-b515-243000a43d34", Mode: "full"}
}

func TestAmbiguousRootJSON(t *testing.T) {
	t.Run("[MAINT-RECOVERY AC-010] duplicate root record fields are rejected", func(t *testing.T) {
		now := time.Now().UTC()
		r := Record{Version: 1, CommandID: testRequest().CommandID, Mode: "full", StartedAt: now, UpdatedAt: now, Phase: "accepted", Outcome: "running"}
		data, _ := json.Marshal(r)
		ambiguous := strings.Replace(string(data), `"outcome":"running"`, `"outcome":"error","outcome":"running"`, 1)
		if _, err := parseRecord([]byte(ambiguous)); err == nil {
			t.Fatal("ambiguous root record accepted")
		}
	})
	t.Run("[MAINT-RECOVERY AC-010] duplicate status and nested version fields fail closed", func(t *testing.T) {
		for _, text := range []string{`{"state":"installed","state":"error"}`, `{"holds":{"versions":{"linux-image":"1","linux-image":"2"}}}`} {
			if err := rejectDuplicateKeys([]byte(text)); err == nil {
				t.Fatal("duplicate root JSON accepted", text)
			}
		}
	})
}
func TestStrictRequest(t *testing.T) {
	valid := `{"version":1,"command_id":"3db2d182-61f4-4a4a-b515-243000a43d34","mode":"full","include_reboot_class":false}`
	for _, mode := range []string{"full", "security", "repair"} {
		t.Run("[MAINT-RECOVERY AC-001] accepts "+mode, func(t *testing.T) {
			r, err := ParseRequest([]byte(strings.Replace(valid, `"full"`, `"`+mode+`"`, 1)))
			if err != nil || r.Mode != mode {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
	cases := map[string]string{
		"unknown version": strings.Replace(valid, `"version":1`, `"version":2`, 1),
		"unknown key":     strings.Replace(valid, `"version":1`, `"version":1,"argv":["sh"]`, 1),
		"duplicate key":   strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1),
		"trailing JSON":   valid + ` {}`, "trailing text": valid + ` nope`, "array": `[` + valid + `]`,
		"wrong bool": strings.Replace(valid, `false`, `"false"`, 1), "null bool": strings.Replace(valid, `false`, `null`, 1),
		"wrong mode": strings.Replace(valid, `"full"`, `["full"]`, 1), "unknown mode": strings.Replace(valid, `"full"`, `"remove"`, 1),
		"UUID injection": strings.Replace(valid, testRequest().CommandID, `../../etc/shadow`, 1), "UUID uppercase": strings.Replace(valid, testRequest().CommandID, strings.ToUpper(testRequest().CommandID), 1),
		"missing mode": strings.Replace(valid, `"mode":"full",`, "", 1), "empty mode": strings.Replace(valid, `"full"`, `""`, 1),
		"repair releases holds": strings.Replace(strings.Replace(valid, `"full"`, `"repair"`, 1), `false`, `true`, 1),
		"oversize":              valid + strings.Repeat(" ", MaxRequestBytes), "bad utf8": valid + string([]byte{255}),
	}
	for name, input := range cases {
		t.Run("[MAINT-RECOVERY AC-001] rejects "+name, func(t *testing.T) {
			if _, err := ParseRequest([]byte(input)); err == nil {
				t.Fatal("unsafe request accepted")
			}
		})
	}
	t.Run("[MAINT-RECOVERY AC-001] canonical marshal round trip", func(t *testing.T) {
		want := testRequest()
		data, _ := json.Marshal(want)
		got, err := ParseRequest(data)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%+v %v", got, err)
		}
	})
}

func TestBoundedCapture(t *testing.T) {
	t.Run("[MAINT-RECOVERY AC-010] caps output and preserves head and latest tail", func(t *testing.T) {
		out, truncated := appendBounded("BEGIN", strings.Repeat("x", MaxOutputBytes)+"END", false)
		if !truncated || len(out) > MaxOutputBytes || !strings.HasPrefix(out, "BEGIN") || !strings.HasSuffix(out, "END") {
			t.Fatalf("capture boundary len=%d", len(out))
		}
		out, truncated = appendBounded(out, "LATEST", truncated)
		if !truncated || len(out) > MaxOutputBytes || !strings.HasSuffix(out, "LATEST") {
			t.Fatal("tail stopped draining")
		}
	})
}

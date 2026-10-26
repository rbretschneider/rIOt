package extraupd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func removeFile(root, rel string) error {
	return os.Remove(filepath.Join(root, filepath.FromSlash(rel)))
}

func TestPolicy_EU_AC016_EncodeIsTheExactInstallerBytes(t *testing.T) {
	if got := string(EncodePolicy([]string{"dietpi"})); got != "{\"version\":1,\"extra_updaters\":[\"dietpi\"]}\n" {
		t.Errorf("opted-in bytes = %q", got)
	}
	if got := string(EncodePolicy(nil)); got != "{\"version\":1,\"extra_updaters\":[]}\n" {
		t.Errorf("revoked bytes = %q", got)
	}
	for _, ids := range [][]string{nil, {"dietpi"}} {
		back, err := ParsePolicy(EncodePolicy(ids))
		if err != nil || len(back) != len(ids) {
			t.Errorf("round trip of %v gave %v, %v", ids, back, err)
		}
	}
}

func TestPolicy_EU_AC018_ParseFailsClosed(t *testing.T) {
	good := `{"version":1,"extra_updaters":["dietpi"]}`
	t.Run("[AC-018] a valid policy parses with and without a trailing newline", func(t *testing.T) {
		for _, body := range []string{good, good + "\n", " " + good + " "} {
			ids, err := ParsePolicy([]byte(body))
			if err != nil || !reflect.DeepEqual(ids, []string{"dietpi"}) {
				t.Errorf("body %q: ids=%v err=%v", body, ids, err)
			}
		}
	})
	bad := map[string]string{
		"empty":                "",
		"not an object":        `["dietpi"]`,
		"version 2":            `{"version":2,"extra_updaters":["dietpi"]}`,
		"version 1.0":          `{"version":1.0,"extra_updaters":["dietpi"]}`,
		"version string":       `{"version":"1","extra_updaters":["dietpi"]}`,
		"missing list":         `{"version":1}`,
		"missing version":      `{"extra_updaters":["dietpi"]}`,
		"unknown key":          `{"version":1,"extra_updaters":["dietpi"],"x":1}`,
		"duplicate key":        `{"version":1,"extra_updaters":["dietpi"],"extra_updaters":[]}`,
		"duplicate version":    `{"version":1,"version":1,"extra_updaters":[]}`,
		"null list":            `{"version":1,"extra_updaters":null}`,
		"list of numbers":      `{"version":1,"extra_updaters":[1]}`,
		"scalar list":          `{"version":1,"extra_updaters":"dietpi"}`,
		"bad identifier":       `{"version":1,"extra_updaters":["Diet Pi"]}`,
		"duplicate identifier": `{"version":1,"extra_updaters":["dietpi","dietpi"]}`,
		"trailing value":       good + ` {}`,
		"trailing garbage":     good + `x`,
		"nine identifiers":     `{"version":1,"extra_updaters":["a","b","c","d","e","f","g","h","i"]}`,
		"oversized":            `{"version":1,"extra_updaters":[],"pad":"` + strings.Repeat("x", MaxPolicyBytes) + `"}`,
		"invalid utf-8":        "{\"version\":1,\"extra_updaters\":[\"\xff\"]}",
		"truncated":            `{"version":1,"extra_updaters":["dietpi"`,
	}
	for name, body := range bad {
		t.Run("[AC-018] rejects "+name, func(t *testing.T) {
			if ids, err := ParsePolicy([]byte(body)); err == nil {
				t.Errorf("body %q parsed to %v, want an error so no updater is enabled", body, ids)
			}
		})
	}
	t.Run("[AC-018] eight valid identifiers are accepted", func(t *testing.T) {
		ids, err := ParsePolicy([]byte(`{"version":1,"extra_updaters":["a","b","c","d","e","f","g","h"]}`))
		if err != nil || len(ids) != 8 {
			t.Errorf("ids=%v err=%v", ids, err)
		}
	})
}

func TestPolicyListsImplemented_EU_AC018_IgnoresUnimplementedIDs(t *testing.T) {
	if !PolicyListsImplemented([]string{"snap", "dietpi"}, "dietpi") {
		t.Error("dietpi must be found")
	}
	if PolicyListsImplemented([]string{"snap"}, "snap") {
		t.Error("an identifier this build does not implement is ignored (AD-003)")
	}
	if PolicyListsImplemented(nil, "dietpi") {
		t.Error("an empty policy enables nothing")
	}
}

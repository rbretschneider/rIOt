package extraupd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Root-side opt-in policy (EXTRA-UPD-001 AD-003). The installer writes it as
// root from fixed literals; the worker enforces it; the agent reads it for
// readiness only. These helpers are the one codec all three share.
const (
	// PolicyPath is the root-owned policy file.
	PolicyPath = "/etc/riot-maintenance/extra-updaters.json"
	// PolicyDropInPath is the unit drop-in that extends the service backstop on
	// opted-in hosts.
	PolicyDropInPath = "/etc/systemd/system/riot-maintenance.service.d/50-riot-extra-updaters.conf"
	// MaxPolicyBytes bounds the policy file (NFR-002).
	MaxPolicyBytes = 512
	// PolicyVersion is the only accepted policy version.
	PolicyVersion = 1
)

// EncodePolicy returns the exact policy bytes for a list of updater IDs. The
// installer test oracle compares against these bytes.
func EncodePolicy(ids []string) []byte {
	if len(ids) == 0 {
		return []byte(`{"version":1,"extra_updaters":[]}` + "\n")
	}
	b, _ := json.Marshal(ids)
	return []byte(fmt.Sprintf(`{"version":%d,"extra_updaters":%s}`+"\n", PolicyVersion, b))
}

// ParsePolicy strictly parses policy bytes: at most MaxPolicyBytes, valid
// UTF-8, exactly the keys version (1) and extra_updaters (at most 8 unique
// identifiers), no duplicate keys, no null, no trailing JSON. Identifiers are
// returned as written; callers drop the ones they do not implement. Any error
// means no updater is enabled.
func ParsePolicy(data []byte) ([]string, error) {
	if len(data) == 0 || len(data) > MaxPolicyBytes || !utf8.Valid(data) {
		return nil, errors.New("invalid policy size or encoding")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if tok, err := d.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("policy must be one object")
	}
	seen := map[string]bool{}
	var ids []string
	for d.More() {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok || seen[key] {
			return nil, errors.New("duplicate policy field")
		}
		seen[key] = true
		var raw json.RawMessage
		if err = d.Decode(&raw); err != nil {
			return nil, err
		}
		switch key {
		case "version":
			var v int
			if err = json.Unmarshal(raw, &v); err != nil || v != PolicyVersion || bytes.ContainsAny(raw, ".eE") {
				return nil, errors.New("unsupported policy version")
			}
		case "extra_updaters":
			if ids, err = parsePolicyIDs(raw); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unknown policy field %q", key)
		}
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("trailing policy JSON")
	}
	if len(seen) != 2 {
		return nil, errors.New("policy requires version and extra_updaters")
	}
	return ids, nil
}

func parsePolicyIDs(raw json.RawMessage) ([]string, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errors.New("null policy list")
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, errors.New("policy list must be an array of strings")
	}
	if len(ids) > models.MaxExtraUpdaters {
		return nil, errors.New("policy lists too many updaters")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !models.ExtraUpdaterIDPattern.MatchString(id) || seen[id] {
			return nil, errors.New("invalid or duplicate updater identifier in policy")
		}
		seen[id] = true
	}
	return ids, nil
}

// PolicyListsImplemented reports whether ids contains id and this build
// implements it.
func PolicyListsImplemented(ids []string, id string) bool {
	if !IsImplemented(id) {
		return false
	}
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

//go:build linux

package collectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
)

// detachedAptHolds reads existing ownership bookkeeping, but keeps its release
// intent in the root worker's record. Root never writes into the agent directory.
type detachedAptHolds struct {
	statePath string
	read      func(string, int64) ([]byte, error)
	run       CommandRunner
}

func NewDetachedAptHolds() maintenance.Holds {
	return &detachedAptHolds{statePath: "/var/lib/riot/holds.json", read: maintenance.ReadAgentFile, run: detachedHoldRunner}
}

func detachedHoldRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	result, err := maintenance.RunHoldCommand(ctx, name, args...)
	if result.Truncated {
		return nil, fmt.Errorf("hold query exceeded output limit")
	}
	return []byte(result.Output), err
}

func (h *detachedAptHolds) state() (holdState, error) {
	var st holdState
	data, err := h.read(h.statePath, 64<<10)
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if len(data) > 64<<10 {
		return st, fmt.Errorf("hold state too large")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return st, fmt.Errorf("invalid hold state object")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return st, err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return st, fmt.Errorf("duplicate hold state field")
		}
		seen[key] = true
		var raw json.RawMessage
		if err = d.Decode(&raw); err != nil {
			return st, err
		}
	}
	if _, err = d.Token(); err != nil {
		return st, err
	}
	if _, err = d.Token(); err != io.EOF {
		return st, fmt.Errorf("trailing hold state data")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&st); err != nil {
		return st, err
	}
	if st.Version != 1 || st.PM != "apt" || len(st.AptHolds) > 256 || len(st.DnfExcludes) > 0 {
		return st, fmt.Errorf("invalid apt hold ownership metadata")
	}
	return st, nil
}

func (h *detachedAptHolds) versions(ctx context.Context) (map[string]string, error) {
	out, err := h.run(ctx, "/usr/bin/dpkg-query", "-W", "-f=${Package}\t${Version}\t${db:Status-Status}\n")
	if err != nil {
		return nil, err
	}
	versions := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) == 3 && f[2] == "installed" && ValidPackageName(f[0]) {
			versions[f[0]] = f[1]
		}
	}
	return versions, nil
}

func validDetachedSnapshot(s maintenance.HoldSnapshot) error {
	if len(s.Packages) > 256 {
		return fmt.Errorf("too many managed holds")
	}
	seen := map[string]bool{}
	for _, p := range s.Packages {
		if !ValidPackageName(p) || ClassifyPackage(p) == "" || seen[p] || s.Versions[p] == "" {
			return fmt.Errorf("invalid managed hold snapshot")
		}
		seen[p] = true
	}
	return nil
}

func (h *detachedAptHolds) Snapshot(ctx context.Context) (maintenance.HoldSnapshot, error) {
	s := maintenance.HoldSnapshot{Versions: map[string]string{}}
	st, err := h.state()
	if err != nil {
		return s, err
	}
	if len(st.AptHolds) == 0 {
		return s, nil
	}
	installed, err := h.versions(ctx)
	if err != nil {
		return s, err
	}
	out, err := h.run(ctx, "/usr/bin/apt-mark", "showhold")
	if err != nil {
		return s, err
	}
	held := map[string]bool{}
	for _, p := range strings.Fields(string(out)) {
		held[p] = true
	}
	seen := map[string]bool{}
	for _, p := range st.AptHolds {
		if !ValidPackageName(p) || ClassifyPackage(p) == "" {
			return s, fmt.Errorf("unsafe managed hold entry")
		}
		if !seen[p] && held[p] && installed[p] != "" {
			s.Packages = append(s.Packages, p)
			s.Versions[p] = installed[p]
			seen[p] = true
		}
	}
	sort.Strings(s.Packages)
	return s, nil
}

func (h *detachedAptHolds) Release(ctx context.Context, s maintenance.HoldSnapshot) error {
	if err := validDetachedSnapshot(s); err != nil {
		return err
	}
	if len(s.Packages) == 0 {
		return nil
	}
	_, err := h.run(ctx, "/usr/bin/apt-mark", append([]string{"unhold"}, s.Packages...)...)
	return err
}

func (h *detachedAptHolds) Restore(ctx context.Context, s maintenance.HoldSnapshot) error {
	if err := validDetachedSnapshot(s); err != nil {
		return err
	}
	if len(s.Packages) == 0 {
		return nil
	}
	installed, err := h.versions(ctx)
	if err != nil {
		return err
	}
	args := []string{"hold"}
	for _, p := range s.Packages {
		if installed[p] != "" {
			args = append(args, p)
		}
	}
	if len(args) == 1 {
		return nil
	}
	_, err = h.run(ctx, "/usr/bin/apt-mark", args...)
	return err
}

func (h *detachedAptHolds) Changes(ctx context.Context, s maintenance.HoldSnapshot) ([]maintenance.VersionChange, error) {
	if err := validDetachedSnapshot(s); err != nil {
		return nil, err
	}
	if len(s.Packages) == 0 {
		return nil, nil
	}
	installed, err := h.versions(ctx)
	if err != nil {
		return nil, err
	}
	var changes []maintenance.VersionChange
	for _, p := range s.Packages {
		if after := installed[p]; after != "" && after != s.Versions[p] {
			changes = append(changes, maintenance.VersionChange{Package: p, Before: s.Versions[p], After: after})
		}
	}
	return changes, nil
}

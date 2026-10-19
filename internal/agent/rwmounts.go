package agent

import (
	"fmt"

	"path/filepath"
)

// strictWritable refuses a stack reachable through any container's writable mount,
// including socket-mounting management containers and bind-backed named volumes.
func (t *composeTrust) strictWritable(snap daemonSnapshot, dir string) error {
	resolved, err := t.fs.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("cannot resolve stack directory")
	}
	if t.trustedPath(resolved) {
		return nil
	}
	state := &checkState{t: t, snap: t.bind(snap), dir: dir, includeManagers: true, metas: map[string]fileMeta{}, typeOK: map[string]bool{}, leafOK: map[string]bool{}, ancOK: map[string]bool{}}
	if !t.windows() {
		state.trust = t.trustSet()
	}
	if refused := state.checkPlacement(resolved); refused != nil {
		return refused
	}
	return nil
}
func (t *composeTrust) trustedPath(path string) bool {
	for _, root := range t.trustedRoots {
		if !filepath.IsAbs(root) {
			continue
		}
		resolved, err := t.fs.EvalSymlinks(root)
		if err == nil && within(resolved, path, t.windows()) {
			return true
		}
	}
	return false
}

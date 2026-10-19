package agent

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// reconcileInterrupted repairs the Docker state an interrupted docker_update
// left behind, from the durable marker (SEC C-4b). It never removes a container.
//
//   - A previous container still under its set-aside name goes back to its
//     original name when nothing holds that name, and is started again if it was
//     running. If something holds the name (the replacement), both are left
//     untouched and named in the summary.
//   - Dependents the update stopped and that are still stopped are restarted.
//   - A sanitized compose file recorded in the marker is removed.
//
// unreachable is true when Docker could not be contacted; the caller keeps the
// marker and tries again at the next start without reporting twice.
func (a *Agent) reconcileInterrupted(ctx context.Context, id string, m inflightMarker) (summary string, unreachable bool) {
	if len(m.Docker) == 0 {
		return "", false
	}
	api, err := a.newDocker()
	if err != nil {
		slog.Warn("recovery: docker unreachable, keeping marker", "command_id", id, "error", err)
		return "", true
	}
	defer api.Close()

	var parts []string
	for _, rec := range m.Docker {
		all, err := api.ContainerList(ctx, container.ListOptions{All: true})
		if err != nil {
			slog.Warn("recovery: cannot list containers, keeping marker", "command_id", id, "error", err)
			return strings.Join(parts, "; "), true
		}
		parts = append(parts, a.reconcileStandalone(ctx, api, rec, all)...)
		parts = append(parts, a.reconcileDependents(ctx, api, rec, all)...)
		if rec.SanitizedFile != "" && filepath.Base(rec.SanitizedFile) == sanitizedComposeName {
			removeSanitized(rec.SanitizedFile)
		}
	}
	summary = strings.Join(parts, "; ")
	if summary != "" {
		slog.Warn("interrupted docker update reconciled", "command_id", id, "action", m.Action, "summary", summary)
	}
	return summary, false
}

// reconcileStandalone puts a set-aside previous container back, if it can be
// done without guessing.
func (a *Agent) reconcileStandalone(ctx context.Context, api dockerAPI, rec dockerRecord, all []container.Summary) []string {
	if rec.OldID == "" || rec.SetAsideName == "" || rec.OriginalName == "" {
		return nil
	}
	var old *container.Summary
	for i := range all {
		if all[i].ID == rec.OldID && summaryHasName(all[i], rec.SetAsideName) {
			old = &all[i]
			break
		}
	}
	if old == nil {
		return nil // already restored, or removed after a verified update
	}
	if _, held := findByName(all, rec.OriginalName); held {
		return []string{fmt.Sprintf("left %s (previous) and %s (replacement) untouched; manual check required",
			rec.SetAsideName, rec.OriginalName)}
	}
	if err := api.ContainerRename(ctx, rec.OldID, rec.OriginalName); err != nil {
		return []string{fmt.Sprintf("could not restore %s from %s: %v", rec.OriginalName, rec.SetAsideName, err)}
	}
	if rec.WasRunning {
		if err := api.ContainerStart(ctx, rec.OldID, container.StartOptions{}); err != nil {
			return []string{fmt.Sprintf("restored %s from %s but could not start it: %v", rec.OriginalName, rec.SetAsideName, err)}
		}
	}
	return []string{fmt.Sprintf("restored %s from %s", rec.OriginalName, rec.SetAsideName)}
}

// reconcileDependents restarts the recorded dependents that exist and are not
// running, with the same helper the update itself uses.
func (a *Agent) reconcileDependents(ctx context.Context, api dockerAPI, rec dockerRecord, all []container.Summary) []string {
	var deps []depRecord
	for _, depID := range rec.StoppedDependentIDs {
		for _, c := range all {
			if c.ID != depID || c.State == "running" {
				continue
			}
			parent := ""
			if ref := containerRef(c.HostConfig.NetworkMode); ref != "" {
				if p, ok := resolveRef(all, ref); ok {
					parent = summaryName(p)
				}
			}
			deps = append(deps, depRecord{ID: c.ID, Name: summaryName(c), ParentName: parent})
		}
	}
	if len(deps) == 0 {
		return nil
	}
	failed := a.restoreDependents(ctx, api, deps)
	failedSet := map[string]bool{}
	for _, f := range failed {
		failedSet[f.Name] = true
	}
	var ok []string
	for _, d := range deps {
		if !failedSet[d.Name] {
			ok = append(ok, d.Name)
		}
	}
	var out []string
	if len(ok) > 0 {
		out = append(out, "restarted dependents: "+strings.Join(ok, ", "))
	}
	if len(failed) > 0 {
		out = append(out, "failed to restart dependents: "+failureReasons(failed))
	}
	return out
}

// cleanupSanitizedLeftovers removes sanitized compose files a crash left behind
// in any compose project directory the Docker daemon knows (SEC C-5): an
// agent-owned regular file is removed, a symlink is unlinked and its target is
// never touched.
func (a *Agent) cleanupSanitizedLeftovers(ctx context.Context) {
	api, err := a.newDocker()
	if err != nil {
		slog.Debug("recovery: docker unavailable, skipping sanitized-file cleanup", "error", err)
		return
	}
	defer api.Close()
	all, err := api.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		slog.Debug("recovery: cannot list containers, skipping sanitized-file cleanup", "error", err)
		return
	}
	seen := map[string]bool{}
	for _, c := range all {
		dir := c.Labels[models.LabelComposeWorkingDir]
		if dir == "" || seen[dir] || !filepath.IsAbs(dir) {
			continue
		}
		seen[dir] = true
		if removeLeftoverSanitized(dir) {
			slog.Info("recovery: removed leftover sanitized compose file", "dir", dir)
		}
	}
}

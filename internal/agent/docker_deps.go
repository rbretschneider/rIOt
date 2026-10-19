package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/docker/docker/api/types/container"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// composeServiceRe is the compose service-name charset. It also rules out a
// leading "-", so a label value can never be read as a flag (SEC C-9).
var composeServiceRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// summaryName is the container's primary name without the leading "/".
func summaryName(c container.Summary) string {
	if len(c.Names) == 0 {
		return ""
	}
	return strings.TrimPrefix(c.Names[0], "/")
}

func summaryHasName(c container.Summary, name string) bool {
	for _, n := range c.Names {
		if strings.TrimPrefix(n, "/") == name {
			return true
		}
	}
	return false
}

// findByName returns the container whose name (without a leading "/") is name.
func findByName(all []container.Summary, name string) (container.Summary, bool) {
	for _, c := range all {
		if summaryHasName(c, name) {
			return c, true
		}
	}
	return container.Summary{}, false
}

// resolveRef finds the container a `container:<ref>` network mode points at:
// by name (with or without a leading "/"), by full ID, or by an ID prefix of at
// least 12 characters.
func resolveRef(all []container.Summary, ref string) (container.Summary, bool) {
	ref = strings.TrimPrefix(ref, "/")
	if ref == "" {
		return container.Summary{}, false
	}
	if c, ok := findByName(all, ref); ok {
		return c, true
	}
	for _, c := range all {
		if c.ID == ref || (len(ref) >= 12 && strings.HasPrefix(c.ID, ref)) {
			return c, true
		}
	}
	return container.Summary{}, false
}

// containerRef returns the target of a `container:<ref>` network mode, or "".
func containerRef(networkMode string) string {
	if !strings.HasPrefix(networkMode, "container:") {
		return ""
	}
	return strings.TrimPrefix(strings.TrimPrefix(networkMode, "container:"), "/")
}

// depRecord is a network dependent that an update stops and must restore
// (FR-028 to FR-033). ParentName is the name of the parent it was attached to,
// used to check the restored dependent points at the current parent.
type depRecord struct {
	ID         string
	Name       string
	ParentName string
	Running    bool
}

// networkDependsOn reports whether dep's network mode references parent: by name
// (with or without a leading "/"), by full container ID, or by compose
// `service:` name within the same project.
func networkDependsOn(dep, parent container.Summary) bool {
	mode := dep.HostConfig.NetworkMode
	switch {
	case strings.HasPrefix(mode, "container:"):
		ref := containerRef(mode)
		if ref == "" {
			return false
		}
		if summaryHasName(parent, ref) || parent.ID == ref {
			return true
		}
		return len(ref) >= 12 && strings.HasPrefix(parent.ID, ref)
	case strings.HasPrefix(mode, "service:"):
		svc := strings.TrimPrefix(mode, "service:")
		project := dep.Labels[models.LabelComposeProject]
		return svc != "" && project != "" &&
			parent.Labels[models.LabelComposeService] == svc &&
			parent.Labels[models.LabelComposeProject] == project
	}
	return false
}

// findDependents returns the containers in all that use one of parents'
// network namespaces. A parent is never its own dependent. Only dependents of
// the given parents are returned, so a single-service update leaves the
// dependents of other services alone (FR-029).
func findDependents(all, parents []container.Summary) []depRecord {
	var out []depRecord
	for _, c := range all {
		for _, p := range parents {
			if c.ID == p.ID {
				continue
			}
			if networkDependsOn(c, p) {
				out = append(out, depRecord{ID: c.ID, Name: summaryName(c), ParentName: summaryName(p), Running: c.State == "running"})
				break
			}
		}
	}
	return out
}

// restoreFailure names a dependent that could not be brought back.
type restoreFailure struct {
	Name   string
	Reason string
}

func failureNames(f []restoreFailure) string {
	names := make([]string, len(f))
	for i, x := range f {
		names[i] = x.Name
	}
	return strings.Join(names, ", ")
}

func failureReasons(f []restoreFailure) string {
	parts := make([]string, len(f))
	for i, x := range f {
		parts[i] = x.Name + ": " + x.Reason
	}
	return strings.Join(parts, "; ")
}

// restoreDependents brings every dependent back: it must end running and
// attached to the network namespace of the current parent (FR-030, BR-007).
// A dependent whose network reference went stale because the parent was
// recreated is rebuilt through compose when it has compose labels; any other
// stale dependent is reported. The caller supplies a context with its own
// budget, independent of the expired operation limit (FR-032).
func (a *Agent) restoreDependents(ctx context.Context, api dockerAPI, deps []depRecord) []restoreFailure {
	var failed []restoreFailure
	fail := func(d depRecord, reason string) {
		reason = models.TruncateHead(reason, 1200)
		slog.Error("docker_update: dependent not restored", "container", d.Name, "reason", reason)
		failed = append(failed, restoreFailure{Name: d.Name, Reason: reason})
	}

	for _, d := range deps {
		all, err := api.ContainerList(ctx, container.ListOptions{All: true})
		if err != nil {
			fail(d, "list containers: "+err.Error())
			continue
		}
		insp, err := api.ContainerInspect(ctx, d.ID)
		if err != nil {
			// Compose may have recreated the dependent under the same name.
			byName, ok := findByName(all, d.Name)
			if !ok {
				fail(d, "container no longer exists: "+err.Error())
				continue
			}
			if insp, err = api.ContainerInspect(ctx, byName.ID); err != nil {
				fail(d, "inspect: "+err.Error())
				continue
			}
		}

		stale, ref := false, ""
		if insp.HostConfig != nil {
			ref = containerRef(string(insp.HostConfig.NetworkMode))
		}
		if ref != "" {
			target, ok := resolveRef(all, ref)
			current, haveCurrent := findByName(all, d.ParentName)
			stale = !ok || (d.ParentName != "" && haveCurrent && target.ID != current.ID)
		}

		switch {
		case stale:
			if insp.Config == nil || insp.Config.Labels[models.LabelComposeService] == "" {
				fail(d, fmt.Sprintf("network parent %q no longer matches the updated container", ref))
				continue
			}
			if err := a.recreateComposeDependent(ctx, api, insp); err != nil {
				fail(d, "recreate through compose: "+err.Error())
				continue
			}
		case insp.State == nil || !insp.State.Running:
			if err := api.ContainerStart(ctx, insp.ID, container.StartOptions{}); err != nil {
				fail(d, "start: "+err.Error())
				continue
			}
		}

		after, err := api.ContainerList(ctx, container.ListOptions{All: true})
		if err != nil {
			fail(d, "verify: "+err.Error())
			continue
		}
		if now, ok := findByName(after, d.Name); !ok || now.State != "running" {
			fail(d, "not running after restore")
		}
	}
	return failed
}

// recreateComposeDependent rebuilds one compose service whose network reference
// is stale: `compose up -d --no-deps --force-recreate -- <service>` with the
// 90s fallback limit (AD-014). The working directory and project come from the
// dependent's labels, so they pass the same trust check as every other compose
// invocation, against a fresh daemon snapshot (AUTOUPD-HARDEN-001 AD-002). A
// refusal is that dependent's restore failure.
func (a *Agent) recreateComposeDependent(ctx context.Context, api dockerAPI, insp container.InspectResponse) error {
	workDir := insp.Config.Labels[models.LabelComposeWorkingDir]
	project := insp.Config.Labels[models.LabelComposeProject]
	service := insp.Config.Labels[models.LabelComposeService]
	if project == "" {
		return errors.New("the dependent has no compose project label")
	}
	snap, err := loadDaemonSnapshot(ctx, api)
	if err != nil {
		return snapshotRefusal(workDir, err)
	}
	td, err := a.composeTrust().Check(snap, trustRequest{Dir: workDir, Project: project, SourceID: insp.ID})
	if err != nil {
		slog.Warn("docker_update: dependent compose directory refused", "container", summaryOfInspect(insp), "refusal", err.Error())
		return err
	}
	args, err := composeArgs(td, project, "", []string{"up", "-d", "--no-deps", "--force-recreate"}, service)
	if err != nil {
		return err
	}
	out, timedOut, err := runLimited(ctx, a.newProc("docker", a.composeChildEnv(), args...), models.DependentComposeUpLimit, models.ChildGrace, a.clock())
	if timedOut {
		return fmt.Errorf("timed out after %s", models.FormatDurationShort(models.DependentComposeUpLimit))
	}
	if err != nil {
		return fmt.Errorf("%w: %s", err, truncateOutput(out, 500))
	}
	return nil
}

// summaryOfInspect names an inspected container for a log line, length-capped.
func summaryOfInspect(insp container.InspectResponse) string {
	return models.TruncateHead(strings.TrimPrefix(insp.Name, "/"), maxRefusalValueBytes)
}

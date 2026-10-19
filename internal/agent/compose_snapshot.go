package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Reasons a container is not a compose source.
const (
	excludedImageLabel  = "image_label"
	excludedImageUnread = "image_uninspectable"
)

// daemonSnapshot is one consistent read of the Docker state the trust check
// needs (AD-013). Every Check takes a fresh one; nothing is cached across calls.
type daemonSnapshot struct {
	DataRoot   string
	Containers []container.Summary
	// excluded maps a container ID to the reason it is not a compose source.
	// Absent means accepted.
	excluded map[string]string
	// volumeDevices maps a local volume name to the host path of its bind
	// device, for volumes created with `o: bind`.
	volumeDevices map[string]string
	// resolve maps a working-dir label to the key of the real directory it names
	// (symlinks resolved). nil means lexical: the cleaned label. Set with
	// composeTrust.bind before labels are compared (QA-D1).
	resolve func(label string) string
}

// loadDaemonSnapshot reads the daemon state within DockerPrecheckBudget. An
// Info, list or VolumeInspect error fails the whole snapshot (fail closed). An
// image that cannot be inspected only excludes the containers that use it.
func loadDaemonSnapshot(ctx context.Context, api dockerAPI) (daemonSnapshot, error) {
	pctx, cancel := context.WithTimeout(ctx, models.DockerPrecheckBudget)
	defer cancel()

	info, err := api.Info(pctx)
	if err != nil {
		return daemonSnapshot{}, fmt.Errorf("docker info: %w", err)
	}
	if info.DockerRootDir == "" {
		return daemonSnapshot{}, errors.New("docker did not report its data root")
	}
	all, err := api.ContainerList(pctx, container.ListOptions{All: true})
	if err != nil {
		return daemonSnapshot{}, fmt.Errorf("list containers: %w", err)
	}
	snap := daemonSnapshot{
		DataRoot: info.DockerRootDir, Containers: all,
		excluded: map[string]string{}, volumeDevices: map[string]string{},
	}

	imageReason := map[string]string{} // image ID → "" accepted, else the reason
	volumeDone := map[string]bool{}
	for _, c := range all {
		if _, has := c.Labels[models.LabelComposeWorkingDir]; has {
			reason, seen := imageReason[c.ImageID]
			if !seen {
				reason = inspectImageProvenance(pctx, api, c.ImageID)
				imageReason[c.ImageID] = reason
			}
			if reason != "" {
				snap.excluded[c.ID] = reason
			}
		}
		for _, m := range c.Mounts {
			if m.Type != mount.TypeVolume || m.Name == "" || volumeDone[m.Name] {
				continue
			}
			volumeDone[m.Name] = true
			v, err := api.VolumeInspect(pctx, m.Name)
			if err != nil {
				return daemonSnapshot{}, fmt.Errorf("inspect volume: %w", err)
			}
			if strings.Contains(v.Options["o"], "bind") && v.Options["device"] != "" {
				snap.volumeDevices[m.Name] = v.Options["device"]
			}
			if m.Driver != "" && m.Driver != "local" {
				for _, key := range []string{"device", "path", "mountpoint"} {
					if path := v.Options[key]; filepath.IsAbs(path) {
						snap.volumeDevices[m.Name] = path
						break
					}
				}
			}
		}
	}
	return snap, nil
}

// inspectImageProvenance returns "" when the image does not define the compose
// working-dir label, and the exclusion reason otherwise. The rule triggers on
// the key being present at all, including an empty value. Compose-built images
// carry only project, service and version labels, so they are unaffected.
func inspectImageProvenance(ctx context.Context, api dockerAPI, imageID string) string {
	if imageID == "" {
		return excludedImageUnread
	}
	img, err := api.ImageInspect(ctx, imageID)
	if err != nil {
		slog.Debug("compose trust: cannot inspect image, excluding its containers", "error", models.TruncateHead(err.Error(), maxRefusalValueBytes))
		return excludedImageUnread
	}
	if img.Config != nil {
		if _, has := img.Config.Labels[models.LabelComposeWorkingDir]; has {
			return excludedImageLabel
		}
	}
	return ""
}

// isSource reports whether a container's compose labels may be believed.
func (s daemonSnapshot) isSource(c container.Summary) bool {
	_, bad := s.excluded[c.ID]
	return !bad
}

// composeSources returns the containers that carry a project and a working
// directory and are not excluded by provenance.
func (s daemonSnapshot) composeSources() []container.Summary {
	var out []container.Summary
	for _, c := range s.Containers {
		if c.Labels[models.LabelComposeProject] != "" && c.Labels[models.LabelComposeWorkingDir] != "" && s.isSource(c) {
			out = append(out, c)
		}
	}
	return out
}

// containerName names a container for a message, falling back to its ID.
func (s daemonSnapshot) containerName(id string) string {
	for _, c := range s.Containers {
		if c.ID == id {
			if n := summaryName(c); n != "" {
				return n
			}
		}
	}
	return id
}

// projectsForDir returns the distinct compose projects of the sources whose
// working directory matches any of dirs (compared by target key, so a trailing
// slash does not matter), sorted.
func (s daemonSnapshot) projectsForDir(dirs ...string) []string {
	keys := map[string]bool{}
	for _, d := range dirs {
		keys[snapKey(s.resolve, d)] = true
	}
	set := map[string]bool{}
	for _, c := range s.composeSources() {
		if keys[s.key(c.Labels[models.LabelComposeWorkingDir])] {
			set[c.Labels[models.LabelComposeProject]] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// resolveProject maps a project name to its one working directory among the
// compose sources, verbatim from the label. Zero or more than one directory is
// a refusal.
func (s daemonSnapshot) resolveProject(project string) (string, *composeTrustError) {
	if !composeProjectRe.MatchString(project) {
		return "", newTrustError(ruleInvalidProject, "", "invalid compose project name %q", project)
	}
	groups := map[string][]string{} // real-directory key -> labels
	for _, c := range s.composeSources() {
		if c.Labels[models.LabelComposeProject] == project {
			wd := c.Labels[models.LabelComposeWorkingDir]
			groups[s.key(wd)] = append(groups[s.key(wd)], wd)
		}
	}
	if len(groups) == 0 {
		return "", newTrustError(ruleProjectNotFound, "", "no container of compose project %q has a trusted working directory", project)
	}
	// One real directory may be named by several labels (a symlink and its
	// target). The smallest label is returned, so the answer is deterministic.
	labels := make([]string, 0, len(groups))
	for _, g := range groups {
		sort.Strings(g)
		labels = append(labels, g[0])
	}
	sort.Strings(labels)
	if len(groups) == 1 {
		return labels[0], nil
	}
	return "", newTrustError(ruleAmbiguousProject, "", "compose project %q has containers in more than one working directory: %s", project, quoteAll(labels))
}

// scopeOf returns the containers of one compose project in a directory,
// optionally narrowed to one service. dirs are the working-dir labels to match
// (the label compose wrote, and the resolved directory it writes after running
// there). A nil excluded matches every container, which is what the
// post-update verification wants.
func scopeOf(all []container.Summary, excluded map[string]string, resolve func(string) string, project string, dirs []string, service string) []container.Summary {
	keys := map[string]bool{}
	for _, d := range dirs {
		keys[snapKey(resolve, d)] = true
	}
	var out []container.Summary
	for _, c := range all {
		if _, bad := excluded[c.ID]; bad {
			continue
		}
		if c.Labels[models.LabelComposeProject] != project || !keys[snapKey(resolve, c.Labels[models.LabelComposeWorkingDir])] {
			continue
		}
		if service != "" && c.Labels[models.LabelComposeService] != service {
			continue
		}
		out = append(out, c)
	}
	return out
}

// snapKey is the comparison key of a working-dir label: the key of the real
// directory when a resolver is bound, else of the cleaned label.
func snapKey(resolve func(string) string, label string) string {
	if resolve != nil {
		return resolve(label)
	}
	return models.TargetKeyCompose(label)
}

func (s daemonSnapshot) key(label string) string { return snapKey(s.resolve, label) }

// bind returns the snapshot with working-dir labels compared by the real
// directory they name. A symlinked stack directory leaves some containers with
// the link in their label and others, recreated by compose running at the
// resolved path, with the target; both are one directory. A label that does not
// resolve (it does not exist, or the agent user cannot traverse it) is compared
// by its cleaned path, so it still counts as its own directory and can make a
// project ambiguous, which fails closed.
func (t *composeTrust) bind(s daemonSnapshot) daemonSnapshot {
	memo := map[string]string{}
	s.resolve = func(label string) string {
		if k, ok := memo[label]; ok {
			return k
		}
		k := models.TargetKeyCompose(filepath.Clean(label))
		if filepath.IsAbs(label) {
			if r, err := t.fs.EvalSymlinks(filepath.Clean(label)); err == nil {
				k = models.TargetKeyCompose(r)
			}
		}
		memo[label] = k
		return k
	}
	return s
}

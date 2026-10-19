package commands

import (
	"context"
	"sort"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// containers returns the device's containers from its latest telemetry, or nil
// when there is none (an unknown device, no Docker, a read error).
func (s *Service) containers(ctx context.Context, deviceID string) []models.ContainerInfo {
	if s.telemetry == nil {
		return nil
	}
	snap, err := s.telemetry.GetLatestSnapshot(ctx, deviceID)
	if err != nil || snap == nil || snap.Data.Docker == nil {
		return nil
	}
	return snap.Data.Docker.Containers
}

// ResolveTargetKeys returns the conflict keys a command holds (AD-004):
//
//   - host actions hold "host";
//   - docker_update holds the compose key of compose_work_dir, or of the
//     container's compose project, or the container-name key, or (when the
//     container is unknown) the container-ID key;
//   - docker_bulk_update holds the union of its targets' keys;
//   - every other action holds nothing and therefore never conflicts.
//
// The agent computes the same keys with the same models helpers.
func (s *Service) ResolveTargetKeys(ctx context.Context, deviceID, action string, params map[string]interface{}) []string {
	switch action {
	case "docker_remove_items", "docker_inventory_refresh":
		return []string{"docker:host"}
	case "docker_start", "docker_stop", "docker_restart":
		if c := findContainer(s.containers(ctx, deviceID), stringParam(params, "container"), stringParam(params, "container_id")); c != nil {
			return []string{models.TargetKeyContainer(strings.TrimLeft(c.Name, "/"))}
		}
		return []string{models.TargetKeyContainer(stringParam(params, "container"))}
	case "docker_stack_pull", "docker_stack_redeploy", "docker_stack_restart", "docker_stack_down", "docker_stack_up":
		return resolveDockerUpdateKeys(params, s.containers(ctx, deviceID))
	case models.ActionDockerUpdate:
		return resolveDockerUpdateKeys(params, s.containers(ctx, deviceID))
	case models.ActionDockerBulkUpdate:
		containers := s.containers(ctx, deviceID)
		var keys []string
		for _, id := range containerIDParams(params) {
			keys = append(keys, containerTargetKey(containers, id))
		}
		return models.NormalizeTargetKeys(keys...)
	default:
		return models.HostTargetKeys(action)
	}
}

// resolveDockerUpdateKeys is the pure part of ResolveTargetKeys for docker_update.
func resolveDockerUpdateKeys(params map[string]interface{}, containers []models.ContainerInfo) []string {
	if wd := stringParam(params, "compose_work_dir"); wd != "" {
		keys := []string{models.TargetKeyCompose(wd)}
		project := stringParam(params, "compose_project")
		for _, c := range containers {
			if c.Labels[models.LabelComposeWorkingDir] == wd && (project == "" || c.Labels[models.LabelComposeProject] == project) {
				keys = append(keys, models.TargetKeyContainer(strings.TrimLeft(c.Name, "/")))
			}
		}
		return models.NormalizeTargetKeys(keys...)
	}
	if project := stringParam(params, "compose_project"); project != "" {
		// A project-only stack update holds the keys of every working directory
		// its containers report in telemetry. Usually that is one; when labels
		// disagree (a symlinked directory after a partial recreate) it is all of
		// them, so a queued duplicate is still a conflict. The agent resolves the
		// real directory and takes its own lock (AUTOUPD-HARDEN-001 AD-011).
		keys := map[string]bool{}
		for _, c := range containers {
			if wd := c.Labels[models.LabelComposeWorkingDir]; wd != "" && c.Labels[models.LabelComposeProject] == project {
				keys[models.TargetKeyCompose(wd)] = true
			}
		}
		list := make([]string, 0, len(keys))
		for k := range keys {
			list = append(list, k)
		}
		sort.Strings(list)
		if len(list) > 0 {
			return list
		}
		return nil
	}
	name := strings.TrimLeft(stringParam(params, "container"), "/")
	id := stringParam(params, "container_id")
	if c := findContainer(containers, name, id); c != nil {
		keys := []string{models.TargetKeyForContainer(*c)}
		if c.Labels[models.LabelComposeWorkingDir] != "" {
			keys = append(keys, models.TargetKeyContainer(strings.TrimLeft(c.Name, "/")))
		}
		return models.NormalizeTargetKeys(keys...)
	}
	switch {
	case name != "":
		return []string{models.TargetKeyContainer(name)}
	case id != "":
		return []string{models.TargetKeyContainerID(id)}
	}
	return nil
}

// containerTargetKey resolves one bulk target (a container ID, as the dashboard
// sends it) to its key.
func containerTargetKey(containers []models.ContainerInfo, id string) string {
	if c := findContainer(containers, "", id); c != nil {
		return models.TargetKeyForContainer(*c)
	}
	return models.TargetKeyContainerID(id)
}

// findContainer matches by name first (leading "/" ignored), then by ID. An ID
// matches exactly or as a prefix of at least 12 characters (the short form
// Docker prints).
func findContainer(containers []models.ContainerInfo, name, id string) *models.ContainerInfo {
	if name != "" {
		for i := range containers {
			if strings.TrimLeft(containers[i].Name, "/") == name {
				return &containers[i]
			}
		}
	}
	if id != "" {
		for i := range containers {
			cid := containers[i].ID
			if cid == id || (len(id) >= 12 && strings.HasPrefix(cid, id)) {
				return &containers[i]
			}
		}
	}
	return nil
}

func stringParam(params map[string]interface{}, key string) string {
	v, _ := params[key].(string)
	return v
}

// containerIDParams returns params["container_ids"] as strings, whether it was
// built in Go ([]string) or decoded from JSON ([]interface{}).
func containerIDParams(params map[string]interface{}) []string {
	switch v := params["container_ids"].(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

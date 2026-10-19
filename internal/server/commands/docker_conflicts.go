package commands

import (
	"context"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"strings"
)

func Applicable(action, state string) bool {
	switch action {
	case "start":
		return state == "exited" || state == "created"
	case "stop", "restart":
		return state == "running" || state == "restarting"
	case "update":
		return state != "dead" && state != "removing"
	}
	return false
}
func (s *Service) DockerConflictSpec(ctx context.Context, device, action string, p map[string]interface{}) (probe []string, docker, exclusive bool) {
	docker = strings.HasPrefix(action, "docker_") && action != "docker_check_updates"
	exclusive = action == "docker_remove_items"
	if action == "docker_start" || action == "docker_stop" || action == "docker_restart" {
		if c := findContainer(s.containers(ctx, device), stringParam(p, "container"), stringParam(p, "container_id")); c != nil {
			if wd := c.Labels[models.LabelComposeWorkingDir]; wd != "" {
				probe = append(probe, models.TargetKeyCompose(wd))
			}
		}
	}
	return
}

package handlers

import (
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"strings"
)

func dockerStackKey(labels map[string]string) string {
	if wd := labels[models.LabelComposeWorkingDir]; wd != "" {
		return models.TargetKeyCompose(wd)
	}
	return ""
}
func dockerNetworkParent(mode string) string {
	for _, p := range []string{"container:", "service:"} {
		if strings.HasPrefix(mode, p) {
			return strings.TrimPrefix(mode, p)
		}
	}
	return ""
}
func dockerProjectionRole(cs []db.FleetContainerProjectionRow, c db.FleetContainerProjectionRow) string {
	if strings.Contains(strings.ToLower(c.Image), "riot-server") {
		return "server"
	}
	if role := c.Labels["riot.role"]; role == "server" || role == "database" {
		return role
	}
	if p := c.Labels[models.LabelComposeProject]; p != "" {
		for _, v := range cs {
			if v.Labels[models.LabelComposeProject] == p && strings.Contains(strings.ToLower(v.Image), "riot-server") {
				return "database"
			}
		}
	}
	return ""
}

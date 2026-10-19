package handlers

import (
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strings"
	"time"
)

func (h *Handlers) validateDockerEventCommand(r *http.Request, e *models.DockerEvent) {
	device := chi.URLParam(r, "id")
	original := e.CommandID
	e.CommandID = ""
	e.CommandAttr = ""
	if h.commandRepo == nil {
		return
	}
	keys := map[string]bool{models.TargetKeyContainer(strings.TrimLeft(e.ContainerName, "/")): true}
	if s, ok := h.dockerSnapshot(r, device); ok {
		for _, c := range s.Data.Docker.Containers {
			if c.Name == e.ContainerName {
				if wd := c.Labels[models.LabelComposeWorkingDir]; wd != "" {
					keys[models.TargetKeyCompose(wd)] = true
				}
			}
		}
	}
	valid := func(c *models.Command) bool {
		if c == nil || c.DeviceID != device {
			return false
		}
		if c.State().IsTerminal() && (c.FinishedAt == nil || h.clock().Sub(*c.FinishedAt) > 10*time.Second) {
			return false
		}
		for _, key := range c.TargetKeys {
			if keys[key] {
				return true
			}
		}
		return false
	}
	if models.IsCanonicalUUID(original) {
		if c, err := h.commandRepo.GetForDevice(r.Context(), original, device); err == nil && valid(c) {
			e.CommandID = c.ID
			e.CommandAttr = "agent"
			return
		}
	}
	active, err := h.commandRepo.ListByDevice(r.Context(), device, 100)
	if err != nil {
		return
	}
	for i := range active {
		if valid(&active[i]) {
			e.CommandID = active[i].ID
			e.CommandAttr = "inferred"
			return
		}
	}
}

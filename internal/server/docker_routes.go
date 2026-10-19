package server

import (
	"github.com/DesyncTheThird/rIOt/internal/server/handlers"
	"github.com/go-chi/chi/v5"
	"net/http"
)

func registerDockerRoutes(r chi.Router, h *handlers.Handlers, auth func(http.Handler) http.Handler) {
	r.Group(func(r chi.Router) {
		r.Use(auth)
		r.Use(h.DockerIdentifiers)
		r.Get("/api/v1/docker/hosts", h.DockerHosts)
		r.Post("/api/v1/docker/bulk-actions", h.DockerBulkActions)
		r.Get("/api/v1/devices/{id}/docker/containers/{name}/logs", h.DockerHistory)
		r.Get("/api/v1/devices/{id}/docker/stacks/{project}/logs", h.DockerHistory)
		r.Get("/api/v1/devices/{id}/docker/containers/{name}/inspect", h.DockerRead)
		r.Get("/api/v1/devices/{id}/docker/stacks/{project}/compose", h.DockerRead)
		r.Get("/api/v1/devices/{id}/docker/stacks/{project}/compose/files/{index}", h.DockerRead)
		r.Get("/api/v1/devices/{id}/docker/stacks/{project}/env", h.DockerRead)
		r.Get("/api/v1/docker/stacks", h.DockerStacks)
		r.Get("/api/v1/docker/stacks/{deviceId}/{project}", h.DockerStackDetail)
		r.Post("/api/v1/devices/{id}/docker/stacks/{project}/actions", h.DockerStackAction)
		r.Post("/api/v1/devices/{id}/docker/stacks/{project}/forget", h.DockerForgetStack)
		r.Post("/api/v1/devices/{id}/docker/stacks/{project}/paths/acknowledge", h.DockerAcknowledgePaths)
		r.Get("/api/v1/docker/inventory", h.DockerInventory)
		r.Post("/api/v1/devices/{id}/docker/inventory/refresh", h.DockerInventoryRefresh)
		r.Post("/api/v1/devices/{id}/docker/prune/preview", h.DockerPrunePreview)
		r.Post("/api/v1/devices/{id}/docker/prune/execute", h.DockerPruneExecute)
		r.Get("/api/v1/docker/activity", h.DockerActivity)
		r.Get("/api/v1/docker/activity/commands/{commandId}", h.DockerActivityDetail)
		r.Get("/ws/docker/logs/{deviceId}", h.DockerLogsWS)
	})
}

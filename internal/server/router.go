package server

import (
	"net/http"

	"github.com/DesyncTheThird/rIOt/internal/server/handlers"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func (s *Server) setupRouter() *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RealIP)
	r.Use(middleware.Logger)
	r.Use(chimw.Recoverer)
	r.Use(middleware.CORS)

	h := handlers.New(s.DeviceRepo, s.TelemetryRepo, s.EventRepo, s.Hub, s.EventGen, s.Config.MasterAPIKey)

	// Health check
	r.Get("/health", h.Health(s.DB))

	// WebSocket
	r.Get("/ws", h.WebSocket)

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		// Registration uses master API key
		r.Post("/devices/register", h.RegisterDevice)

		// Device-specific endpoints require device auth
		r.Route("/devices/{id}", func(r chi.Router) {
			r.Use(middleware.DeviceAuth(s.DeviceRepo))
			r.Post("/heartbeat", h.Heartbeat)
			r.Post("/telemetry", h.Telemetry)
		})

		// Dashboard/read endpoints (no auth in Phase 1 for dashboard)
		r.Get("/devices", h.ListDevices)
		r.Get("/devices/{id}", h.GetDevice)
		r.Get("/devices/{id}/history", h.GetDeviceHistory)
		r.Delete("/devices/{id}", h.DeleteDevice)
		r.Get("/summary", h.Summary)
		r.Get("/events", h.ListEvents)
	})

	// Serve embedded frontend
	if s.FrontendFS != nil {
		s.serveFrontend(r)
	}

	return r
}

func (s *Server) serveFrontend(r *chi.Mux) {
	fileServer := http.FileServer(http.FS(s.FrontendFS))
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file; if not found, serve index.html for SPA routing
		path := r.URL.Path[1:]
		if path == "" {
			path = "index.html"
		}
		f, err := s.FrontendFS.Open(path)
		if err != nil {
			r.URL.Path = "/"
		} else {
			f.Close()
		}
		fileServer.ServeHTTP(w, r)
	})
}

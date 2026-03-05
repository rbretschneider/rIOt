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
	r.Use(middleware.CORS(s.Config.AllowedOrigins))

	h := handlers.New(handlers.HandlerDeps{
		Devices:           s.DeviceRepo,
		Telemetry:         s.TelemetryRepo,
		Events:            s.EventRepo,
		Hub:               s.Hub,
		EventGen:          s.EventGen,
		UpdateChecker:     s.UpdateChecker,
		MasterAPIKey:      s.Config.MasterAPIKey,
		AdminRepo:         s.AdminRepo,
		TerminalRepo:      s.TerminalRepo,
		AlertRuleRepo:     s.AlertRuleRepo,
		NotifyRepo:        s.NotifyRepo,
		Dispatcher:        s.Dispatcher,
		CommandRepo:       s.CommandRepo,
		ProbeRepo:         s.ProbeRepo,
		ProbeRunner:       s.ProbeRunner,
		JWTSecret:         s.JWTSecret,
		AdminPasswordHash: s.Config.AdminPasswordHash,
	})

	// Rate limiters
	loginLimiter := middleware.NewRateLimiter(5, 5)     // 5/min
	registerLimiter := middleware.NewRateLimiter(10, 10) // 10/min

	// === PUBLIC routes (no auth) ===
	r.Get("/health", h.Health(s.DB))

	r.Route("/api/v1/auth", func(r chi.Router) {
		r.With(loginLimiter.Middleware()).Post("/login", h.Login)
		r.Post("/logout", h.Logout)
		r.Get("/check", h.AuthCheck)
	})

	// === AGENT routes (device key auth via X-rIOt-Key) ===
	r.Route("/api/v1", func(r chi.Router) {
		r.With(registerLimiter.Middleware()).Post("/devices/register", h.RegisterDevice)

		r.Route("/devices/{id}", func(r chi.Router) {
			r.Use(middleware.DeviceAuth(s.DeviceRepo))
			r.Post("/heartbeat", h.Heartbeat)
			r.Post("/telemetry", h.Telemetry)
			r.Post("/docker-events", h.ReceiveDockerEvent)
		})

		r.Get("/update/check", h.AgentUpdateCheck)
	})

	r.Get("/ws/agent", h.HandleAgentWS)

	// === ADMIN routes (JWT cookie auth) ===
	adminAuth := middleware.AdminAuth(s.JWTSecret)

	r.Group(func(r chi.Router) {
		r.Use(adminAuth)

		r.Get("/ws", h.WebSocket)
		r.Get("/ws/terminal/{deviceId}/{containerId}", h.HandleTerminalWS)

		r.Route("/api/v1", func(r chi.Router) {
			r.Get("/devices", h.ListDevices)
			r.Get("/devices/{id}", h.GetDevice)
			r.Get("/devices/{id}/history", h.GetDeviceHistory)
			r.Get("/devices/{id}/containers", h.GetDeviceContainers)
			r.Get("/devices/{id}/containers/{cid}", h.GetContainerDetail)
			r.Delete("/devices/{id}", h.DeleteDevice)
			r.Post("/devices/{id}/rotate-key", h.RotateKey)
			r.Post("/devices/{id}/commands", h.SendCommand)
			r.Get("/devices/{id}/commands", h.ListDeviceCommands)
			r.Get("/summary", h.Summary)
			r.Get("/events", h.ListEvents)
			r.Get("/update/server", h.ServerUpdateCheck)

			// Settings: alert rules
			r.Route("/settings/alert-rules", func(r chi.Router) {
				r.Get("/", h.ListAlertRules)
				r.Post("/", h.CreateAlertRule)
				r.Put("/{id}", h.UpdateAlertRule)
				r.Delete("/{id}", h.DeleteAlertRule)
			})

			// Settings: notification channels
			r.Route("/settings/notification-channels", func(r chi.Router) {
				r.Get("/", h.ListNotificationChannels)
				r.Post("/", h.CreateNotificationChannel)
				r.Put("/{id}", h.UpdateNotificationChannel)
				r.Delete("/{id}", h.DeleteNotificationChannel)
				r.Post("/{id}/test", h.TestNotificationChannel)
			})

			// Settings: notification log
			r.Get("/settings/notifications/log", h.ListNotificationLog)

			// Fleet management
			r.Get("/fleet/agent-versions", h.AgentVersionSummary)
			r.Post("/fleet/bulk-update", h.BulkUpdateAgents)

			// Security
			r.Get("/security/overview", h.SecurityOverview)
			r.Get("/security/devices", h.SecurityDevices)

			// Probes
			r.Route("/probes", func(r chi.Router) {
				r.Get("/", h.ListProbes)
				r.Post("/", h.CreateProbe)
				r.Put("/{id}", h.UpdateProbe)
				r.Delete("/{id}", h.DeleteProbe)
				r.Post("/{id}/run", h.RunProbe)
				r.Get("/{id}/results", h.GetProbeResults)
			})
		})
	})

	// Serve embedded frontend (must come last as catch-all)
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

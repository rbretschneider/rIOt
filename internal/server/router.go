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

	// Setup guard: blocks non-setup API routes until setup is complete
	r.Use(middleware.SetupGuard(&s.SetupComplete))

	// Set up enrollment handler if mTLS is enabled
	var enrollH *handlers.EnrollHandler
	if s.Config.MTLSEnabled && s.CA != nil {
		enrollH = handlers.NewEnrollHandler(s.CA, s.CARepo, s.DeviceRepo)
	}

	h := handlers.New(handlers.HandlerDeps{
		Devices:           s.DeviceRepo,
		Telemetry:         s.TelemetryRepo,
		Events:            s.EventRepo,
		Hub:               s.Hub,
		EventGen:          s.EventGen,
		UpdateChecker:     s.UpdateChecker,
		AdminRepo:         s.AdminRepo,
		TerminalRepo:      s.TerminalRepo,
		AlertRuleRepo:     s.AlertRuleRepo,
		NotifyRepo:        s.NotifyRepo,
		Dispatcher:        s.Dispatcher,
		CommandRepo:       s.CommandRepo,
		ProbeRepo:         s.ProbeRepo,
		ProbeRunner:       s.ProbeRunner,
		LogRepo:           s.LogRepo,
		DeviceLogRepo:     s.DeviceLogRepo,
		AutoUpdateRepo:       s.AutoUpdateRepo,
		ContainerLogRepo:     s.ContainerLogRepo,
		ContainerMetricRepo:  s.ContainerMetricRepo,
		DeviceProbeRepo:      s.DeviceProbeRepo,
		JWTSecret:         s.JWTSecret,
		AdminPasswordHash: s.Config.AdminPasswordHash,
	})

	// Setup wizard handler
	setupH := handlers.NewSetupHandler(s.AdminRepo, s.CARepo, s.applyTLSAndRestart)

	// Rate limiters
	loginLimiter := middleware.NewRateLimiter(5, 5)     // 5/min
	registerLimiter := middleware.NewRateLimiter(10, 10) // 10/min

	// === PUBLIC routes (no auth) ===
	r.Get("/health", h.Health(s.DB))

	// Setup wizard routes
	r.Route("/api/v1/setup", func(r chi.Router) {
		r.Get("/status", setupH.Status)
		r.With(loginLimiter.Middleware()).Post("/complete", setupH.Complete)
	})

	r.Route("/api/v1/auth", func(r chi.Router) {
		r.With(loginLimiter.Middleware()).Post("/login", h.Login)
		r.Post("/logout", h.Logout)
		r.Get("/check", h.AuthCheck)
	})

	// === PUBLIC routes (agent TOFU) ===
	r.Get("/api/v1/server-cert", h.ServerCert)

	// === AGENT routes (device key auth via X-rIOt-Key) ===
	r.With(registerLimiter.Middleware()).Post("/api/v1/devices/register", h.RegisterDevice)
	r.Get("/api/v1/update/check", h.AgentUpdateCheck)
	if s.Config.MTLSEnabled && s.CA != nil {
		r.With(middleware.MTLSDeviceAuth(s.DeviceRepo, s.CARepo)).Get("/ws/agent", h.HandleAgentWS)
	} else {
		r.Get("/ws/agent", h.HandleAgentWS)
	}

	// mTLS enrollment routes (public, bootstrap key auth)
	if enrollH != nil {
		r.Post("/api/v1/enroll", enrollH.Enroll)
		r.Get("/api/v1/ca.pem", enrollH.CACert)
		// mTLS certificate renewal (requires valid client cert)
		r.With(middleware.MTLSDeviceAuth(s.DeviceRepo, s.CARepo)).Post("/api/v1/renew", enrollH.Renew)
	}

	// Build device auth middleware stack: API key always required,
	// client cert also required when mTLS is enabled.
	deviceAuth := []func(http.Handler) http.Handler{middleware.DeviceAuth(s.DeviceRepo)}
	if s.Config.MTLSEnabled && s.CA != nil {
		deviceAuth = append([]func(http.Handler) http.Handler{
			middleware.MTLSDeviceAuth(s.DeviceRepo, s.CARepo),
		}, deviceAuth...)
	}

	r.Route("/api/v1/devices/{id}", func(r chi.Router) {
		// Agent-authenticated endpoints (mTLS + API key when mTLS enabled)
		r.With(deviceAuth...).Post("/heartbeat", h.Heartbeat)
		r.With(deviceAuth...).Post("/telemetry", h.Telemetry)
		r.With(deviceAuth...).Post("/docker-events", h.ReceiveDockerEvent)
		r.With(deviceAuth...).Post("/events", h.ReceiveAgentEvent)
		r.With(deviceAuth...).Post("/logs", h.ReceiveDeviceLogs)

		// Admin-authenticated endpoints
		adminAuth := middleware.AdminAuth(s.JWTSecret)
		r.With(adminAuth).Get("/", h.GetDevice)
		r.With(adminAuth).Get("/history", h.GetDeviceHistory)
		r.With(adminAuth).Get("/containers", h.GetDeviceContainers)
		r.With(adminAuth).Get("/containers/{cid}", h.GetContainerDetail)
		r.With(adminAuth).Get("/containers/{cname}/metrics", h.GetContainerMetricHistory)
		r.With(adminAuth).Get("/containers/{cid}/logs", h.GetContainerLogs)
		r.With(adminAuth).Put("/location", h.UpdateDeviceLocation)
		r.With(adminAuth).Put("/tags", h.UpdateDeviceTags)
		r.With(adminAuth).Get("/alert-rules", h.GetDeviceAlertRules)
		r.With(adminAuth).Get("/heartbeats", h.GetHeartbeatHistory)
		r.With(adminAuth).Get("/logs", h.GetDeviceLogs)
		r.With(adminAuth).Delete("/", h.DeleteDevice)
		r.With(adminAuth).Post("/rotate-key", h.RotateKey)
		r.With(adminAuth).Post("/commands", h.SendCommand)
		r.With(adminAuth).Get("/commands", h.ListDeviceCommands)
		r.With(adminAuth).Get("/commands/{commandId}/output", h.GetCommandOutput)
		r.With(adminAuth).Post("/docker/bulk-update", h.BulkDockerUpdate)
		r.With(adminAuth).Put("/auto-patch", h.SetAutoPatch)
		r.With(adminAuth).Get("/auto-updates", h.ListAutoUpdates)
		r.With(adminAuth).Put("/auto-updates", h.SetAutoUpdate)
		r.With(adminAuth).Delete("/auto-updates/{target}", h.DeleteAutoUpdate)
		r.With(adminAuth).Get("/security-score", h.GetSecurityScore)

		// Device probes
		r.With(adminAuth).Get("/device-probes", h.ListDeviceProbes)
		r.With(adminAuth).Post("/device-probes", h.CreateDeviceProbe)
		r.With(adminAuth).Put("/device-probes/{pid}", h.UpdateDeviceProbe)
		r.With(adminAuth).Delete("/device-probes/{pid}", h.DeleteDeviceProbe)
		r.With(adminAuth).Post("/device-probes/{pid}/run", h.RunDeviceProbe)
		r.With(adminAuth).Get("/device-probes/{pid}/results", h.GetDeviceProbeResults)
		r.With(deviceAuth...).Post("/probe-results", h.ReceiveDeviceProbeResults)
	})

	// === ADMIN routes (JWT cookie auth) ===
	adminAuth := middleware.AdminAuth(s.JWTSecret)

	r.Group(func(r chi.Router) {
		r.Use(adminAuth)

		r.Post("/api/v1/auth/change-password", h.ChangePassword)

		r.Get("/ws", h.WebSocket)
		r.Get("/ws/terminal/{deviceId}/{containerId}", h.HandleTerminalWS)

		r.Get("/api/v1/devices", h.ListDevices)
		r.Get("/api/v1/summary", h.Summary)
		r.Get("/api/v1/events", h.ListEvents)
		r.Get("/api/v1/events/unread-count", h.UnreadEventCount)
		r.Post("/api/v1/events/{id}/acknowledge", h.AcknowledgeEvent)
		r.Post("/api/v1/events/acknowledge-all", h.AcknowledgeAllEvents)
		r.Get("/api/v1/update/server", h.ServerUpdateCheck)

		// Settings: alert rules & templates
		r.Get("/api/v1/settings/alert-templates", h.ListAlertTemplates)
		r.Get("/api/v1/settings/alert-rules", h.ListAlertRules)
		r.Post("/api/v1/settings/alert-rules", h.CreateAlertRule)
		r.Put("/api/v1/settings/alert-rules/{id}", h.UpdateAlertRule)
		r.Delete("/api/v1/settings/alert-rules/{id}", h.DeleteAlertRule)

		// Settings: notification channels
		r.Get("/api/v1/settings/notification-channels", h.ListNotificationChannels)
		r.Post("/api/v1/settings/notification-channels", h.CreateNotificationChannel)
		r.Put("/api/v1/settings/notification-channels/{id}", h.UpdateNotificationChannel)
		r.Delete("/api/v1/settings/notification-channels/{id}", h.DeleteNotificationChannel)
		r.Post("/api/v1/settings/notification-channels/{id}/test", h.TestNotificationChannel)

		// Settings: notification log
		r.Get("/api/v1/settings/notifications/log", h.ListNotificationLog)

		// Settings: feature toggles
		r.Get("/api/v1/settings/features", h.GetFeatureToggles)
		r.Put("/api/v1/settings/features", h.SetFeatureToggles)

		// Settings: server logs
		r.Get("/api/v1/settings/logs", h.GetServerLogs)

		// Settings: device registration
		r.Get("/api/v1/settings/registration", h.GetRegistrationKey)
		r.Put("/api/v1/settings/registration", h.SetRegistrationKey)

		// Settings: certificates (mTLS)
		if enrollH != nil {
			r.Get("/api/v1/settings/certs", enrollH.ListCerts)
			r.Post("/api/v1/settings/certs/{serial}/revoke", enrollH.RevokeCert)
			r.Get("/api/v1/settings/bootstrap-keys", enrollH.ListBootstrapKeys)
			r.Post("/api/v1/settings/bootstrap-keys", enrollH.CreateBootstrapKey)
			r.Delete("/api/v1/settings/bootstrap-keys/{hash}", enrollH.DeleteBootstrapKey)
		}

		// Settings: TLS certificate management
		r.Get("/api/v1/settings/tls/sans", setupH.GetTLSSANs)
		r.Put("/api/v1/settings/tls/sans", setupH.SetTLSSANs)
		r.Post("/api/v1/settings/tls/regenerate", setupH.RegenerateTLS)

		// Fleet management
		r.Get("/api/v1/fleet/agent-versions", h.AgentVersionSummary)
		r.Get("/api/v1/fleet/patch-status", h.PatchStatus)
		r.Post("/api/v1/fleet/bulk-update", h.BulkUpdateAgents)
		r.Post("/api/v1/fleet/bulk-patch", h.BulkPatchDevices)

		// Security
		r.Get("/api/v1/security/overview", h.SecurityOverview)
		r.Get("/api/v1/security/devices", h.SecurityDevices)

		// Probes
		r.Get("/api/v1/probes", h.ListProbes)
		r.Post("/api/v1/probes", h.CreateProbe)
		r.Put("/api/v1/probes/{id}", h.UpdateProbe)
		r.Delete("/api/v1/probes/{id}", h.DeleteProbe)
		r.Post("/api/v1/probes/{id}/run", h.RunProbe)
		r.Get("/api/v1/probes/{id}/results", h.GetProbeResults)
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

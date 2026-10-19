package server

import (
	"net/http"
	"path"

	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/handlers"
	"github.com/DesyncTheThird/rIOt/internal/server/ingest"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/DesyncTheThird/rIOt/internal/server/oidc"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// globalMiddleware returns the middleware chain applied to every request, in
// order. It is a single function so router tests exercise exactly the chain
// production uses (HARDEN-002 AD-013).
//
// OIDC-001 AD-020: chimw.RealIP is replaced with a trusted-proxy-gated
// client-identity resolver. RIOT_TRUSTED_PROXIES defaults to empty
// (trust nobody), so forwarding headers are ignored unless the
// immediate peer is explicitly trusted — this closes the pre-existing
// bypass where an unconditional chimw.RealIP made loginLimiter and
// registerLimiter keyable on an attacker-supplied header.
//
// HARDEN-002 AD-003: SecurityHeaders runs second, after RealIP (HSTS needs
// the trusted-proxy scheme) and before everything that can write a response
// (CORS 204, SetupGuard 503, auth 401/403, Recoverer 500), and never wraps the
// ResponseWriter.
func (s *Server) globalMiddleware() []func(http.Handler) http.Handler {
	trustedProxies := middleware.ParseTrustedProxies(s.Config.TrustedProxies)
	return []func(http.Handler) http.Handler{
		middleware.RealIP(trustedProxies),
		middleware.SecurityHeaders(),
		middleware.Logger,
		chimw.Recoverer,
		middleware.CORS(s.Config.AllowedOrigins),
		// Setup guard: blocks non-setup API routes until setup is complete
		middleware.SetupGuard(&s.SetupComplete),
	}
}

func (s *Server) setupRouter() *chi.Mux {
	r := chi.NewRouter()

	// Global middleware (HARDEN-002 AD-013: one definition, shared with tests).
	r.Use(s.globalMiddleware()...)

	// Set up enrollment handler if mTLS is enabled
	var enrollH *handlers.EnrollHandler
	if s.Config.MTLSEnabled && s.CA != nil {
		enrollH = handlers.NewEnrollHandler(s.CA, s.CARepo, s.DeviceRepo)
	}

	// OIDC-001: constructed once and lives for the process lifetime, which is
	// what makes the discovery cache useful (AD-004). New performs no
	// network I/O — dormant/malformed-issuer configurations never contact
	// the IdP (FR-006).
	oidcSvc := oidc.New(oidc.Options{
		IssuerURL:    s.Config.OIDCIssuerURL,
		ClientID:     s.Config.OIDCClientID,
		ClientSecret: s.Config.OIDCClientSecret,
		ButtonLabel:  s.Config.OIDCButtonLabel,
		JWTSecret:    s.JWTSecret,
	})

	// REG-HARDEN-001: the registration handler may accept a client certificate
	// as proof of device identity only when mTLS is active. Assign s.CARepo
	// into the interface only inside the guard: a nil *db.CARepo stored in the
	// interface would be a non-nil interface (typed-nil), so an inactive-mTLS
	// router would wrongly look certificate-capable.
	var regCARepo db.CARepository
	if s.Config.MTLSEnabled && s.CA != nil {
		regCARepo = s.CARepo
	}

	h := handlers.New(handlers.HandlerDeps{
		Devices:                   s.DeviceRepo,
		Telemetry:                 s.TelemetryRepo,
		Events:                    s.EventRepo,
		Hub:                       s.Hub,
		EventGen:                  s.EventGen,
		UpdateChecker:             s.UpdateChecker,
		AdminRepo:                 s.AdminRepo,
		TerminalRepo:              s.TerminalRepo,
		AlertRuleRepo:             s.AlertRuleRepo,
		NotifyRepo:                s.NotifyRepo,
		Dispatcher:                s.Dispatcher,
		CommandRepo:               s.CommandRepo,
		Commands:                  s.Commands,
		ProbeRepo:                 s.ProbeRepo,
		ProbeRunner:               s.ProbeRunner,
		LogRepo:                   s.LogRepo,
		DeviceLogRepo:             s.DeviceLogRepo,
		AutoUpdateRepo:            s.AutoUpdateRepo,
		ContainerLogRepo:          s.ContainerLogRepo,
		DockerRepo:                db.NewDockerRepo(s.DB),
		ContainerLogRetentionDays: s.EffectiveContainerLogRetention(),
		StorageQuota:              s.StorageQuota,
		PartitionManager:          s.PartitionManager,
		StorageGuard:              s.StorageGuard,
		ContainerMetricRepo:       s.ContainerMetricRepo,
		DeviceProbeRepo:           s.DeviceProbeRepo,
		JWTSecret:                 s.JWTSecret,
		AdminPasswordHash:         s.Config.AdminPasswordHash,
		OIDC:                      oidcSvc,
		ExternalIdentityRepo:      s.ExternalIdentityRepo,
		SetupComplete:             &s.SetupComplete,
		CARepo:                    regCARepo,
	})

	// Setup wizard handler
	setupH := handlers.NewSetupHandler(s.AdminRepo, s.CARepo, s.applyTLSAndRestart)

	// Rate limiters
	loginLimiter := middleware.NewRateLimiter(5, 5)      // 5/min
	registerLimiter := middleware.NewRateLimiter(10, 10) // 10/min
	// OIDC-001 AD-013: a separate bucket from loginLimiter (same policy) so a
	// burst of failed SSO attempts cannot consume the operator's
	// password-login allowance and defeat the D-9 password fallback.
	oidcLimiter := middleware.NewRateLimiter(5, 5) // 5/min, same policy as loginLimiter

	// === PUBLIC routes (no auth) ===
	// HARDEN-002 AD-011: /health returns 503 when degraded; /readyz also
	// requires migrations to have completed. Only assign s.DB when non-nil: a
	// typed-nil *db.DB inside the interface would panic on probe.
	hh := &handlers.HealthHandler{Migrated: s.MigrationsComplete.Load}
	if s.DB != nil {
		hh.DB = s.DB
	}
	r.Get("/health", hh.Health)
	r.Get("/readyz", hh.Readyz)

	// GROWTH-001 AD-030 / SEC-032: every cookie-authenticated route takes this
	// one chain (origin check, then the session check). It is attached per
	// route or group, never to the shared /api/v1/devices/{id} route, which
	// also carries the agent POSTs: agents authenticate with a device key or
	// client certificate, send no Origin, and must not be refused. The origin
	// check runs before the session check, so an unauthenticated cross-origin
	// write gets 403, not 401.
	adminAuth := middleware.AdminAuthSameOrigin(s.JWTSecret, s.Config.AllowedOrigins)
	registerDockerRoutes(r, h, adminAuth)

	// Setup wizard routes
	r.Route("/api/v1/setup", func(r chi.Router) {
		// Setup and login run before any session exists, so they cannot use
		// adminAuth. They need the origin check alone (login CSRF).
		r.Use(middleware.SameOrigin(s.Config.AllowedOrigins))
		r.Get("/status", setupH.Status)
		r.With(loginLimiter.Middleware()).Post("/complete", setupH.Complete)
	})

	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Use(middleware.SameOrigin(s.Config.AllowedOrigins))
		r.With(loginLimiter.Middleware()).Post("/login", h.Login)
		r.Post("/logout", h.Logout)
		r.Get("/check", h.AuthCheck)

		// OIDC-001: routes are always registered; dormancy is a
		// handler-level 404 (AD-011) — the frontend catch-all at the bottom
		// of this file serves index.html with HTTP 200 for any path chi
		// does not match, so conditional registration would make AC-002/
		// AC-020 fail with a 200 HTML page instead of a 404.
		r.Get("/oidc", h.OIDCAvailability)
		r.With(oidcLimiter.MiddlewareRedirect("/?sso_error=sso_failed")).Get("/oidc/start", h.OIDCStart)
		r.With(oidcLimiter.MiddlewareRedirect("/?sso_error=sso_failed")).Get("/oidc/callback", h.OIDCCallback)
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

	deviceAuth = append(deviceAuth, ingest.New().Middleware)
	r.Route("/api/v1/devices/{id}", func(r chi.Router) {
		// Agent-authenticated endpoints (mTLS + API key when mTLS enabled)
		r.With(deviceAuth...).Post("/heartbeat", h.Heartbeat)
		r.With(deviceAuth...).Post("/telemetry", h.Telemetry)
		r.With(deviceAuth...).Post("/docker-events", h.ReceiveDockerEvent)
		r.With(deviceAuth...).Post("/events", h.ReceiveAgentEvent)
		r.With(deviceAuth...).Post("/logs", h.ReceiveDeviceLogs)

		// Admin-authenticated endpoints
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
		registerCommandRoutes(r, h, deviceAuth, adminAuth)
		r.With(adminAuth).Put("/auto-patch", h.SetAutoPatch)
		r.With(adminAuth).Get("/auto-updates", h.ListAutoUpdates)
		r.With(adminAuth).Put("/auto-updates", h.SetAutoUpdate)
		r.With(adminAuth).Delete("/auto-updates/{target}", h.DeleteAutoUpdate)
		r.With(adminAuth).Get("/security-score", h.GetSecurityScore)
		r.With(adminAuth).Get("/summary", h.DeviceSummary)

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
	// HARDEN-002 AD-001: profiling is never served on this listener. The
	// explicit 404 keeps the SPA catch-all from answering these paths with
	// index.html and 200. Profiles are available only on the opt-in loopback
	// listener (RIOT_PPROF_ADDR, see pprof.go).
	r.Handle("/debug/pprof", pprofNotFound)
	r.Handle("/debug/pprof/*", pprofNotFound)

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
		r.Delete("/api/v1/settings/logs", h.DeleteServerLogs)

		// Settings: server error-rate alert config
		r.Get("/api/v1/settings/server-error-alert", h.GetServerErrorAlert)
		r.Put("/api/v1/settings/server-error-alert", h.SetServerErrorAlert)

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

		// Settings: automation intervals
		r.Get("/api/v1/settings/automation", h.GetAutomationConfig)
		r.Put("/api/v1/settings/automation", h.SetAutomationConfig)

		// Fleet management
		r.Get("/api/v1/fleet/agent-versions", h.AgentVersionSummary)
		r.Get("/api/v1/fleet/patch-status", h.PatchStatus)
		r.Post("/api/v1/fleet/bulk-update", h.BulkUpdateAgents)
		r.Post("/api/v1/fleet/bulk-patch", h.BulkPatchDevices)

		// Fleet dashboard (FLEET-DASH)
		r.Get("/api/v1/fleet/heartbeats", h.FleetHeartbeats)
		r.Get("/api/v1/fleet/containers", h.FleetContainers)

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

		// Device probes (all devices, aggregated)
		r.Get("/api/v1/device-probes", h.ListAllDeviceProbes)
	})

	// Serve embedded frontend (must come last as catch-all)
	if s.FrontendFS != nil {
		s.serveFrontend(r)
	}

	return r
}

// staticContentTypes pins the Content-Type of embedded frontend assets.
// With X-Content-Type-Options: nosniff the browser refuses scripts and styles
// with the wrong type, and Go's built-in MIME table is OS-dependent (the
// Windows registry can override .js; .ico, .woff2, .woff, .ttf and
// .webmanifest are missing, and sniffing turns .webmanifest into text/plain).
// Unknown extensions are left to http.FileServer (HARDEN-002 AD-005).
var staticContentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".map":         "application/json",
	".webmanifest": "application/manifest+json",
	".ico":         "image/x-icon",
	".png":         "image/png",
	".svg":         "image/svg+xml",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".webp":        "image/webp",
	".woff2":       "font/woff2",
	".woff":        "font/woff",
	".ttf":         "font/ttf",
	".txt":         "text/plain; charset=utf-8",
}

func (s *Server) serveFrontend(r *chi.Mux) {
	fileServer := http.FileServer(http.FS(s.FrontendFS))
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file; if not found, serve index.html for SPA routing
		name := r.URL.Path[1:]
		if name == "" {
			name = "index.html"
		}
		f, err := s.FrontendFS.Open(name)
		if err != nil {
			r.URL.Path = "/"
			name = "index.html"
		} else {
			f.Close()
		}
		if ct, ok := staticContentTypes[path.Ext(name)]; ok {
			w.Header().Set("Content-Type", ct)
		}
		fileServer.ServeHTTP(w, r)
	})
}

// registerCommandRoutes mounts the command routes under /api/v1/devices/{id}.
// It is a function of its auth stacks so the auth matrix is testable
// (TestCL_SEC_RouteAuthMatrix): the admin routes take the JWT cookie, and the
// agent report route takes the full deviceAuth stack (the API key, plus the
// client certificate when mTLS is enabled).
func registerCommandRoutes(r chi.Router, h *handlers.Handlers, deviceAuth []func(http.Handler) http.Handler, adminAuth func(http.Handler) http.Handler) {
	r.With(adminAuth).Post("/commands", h.SendCommand)
	r.With(adminAuth).Get("/commands", h.ListDeviceCommands)
	r.With(adminAuth).Get("/commands/{commandId}", h.GetCommand)
	r.With(adminAuth).Post("/commands/{commandId}/cancel", h.CancelCommand)
	r.With(adminAuth).Get("/commands/{commandId}/output", h.GetCommandOutput)
	r.With(deviceAuth...).Post("/commands/{commandId}/report", h.AgentCommandReport)
	r.With(adminAuth).Post("/docker/bulk-update", h.BulkDockerUpdate)
}

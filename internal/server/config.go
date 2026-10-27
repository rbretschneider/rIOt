package server

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	Port                      int
	DBUrl                     string
	RegistrationKey           string // optional key to gate device registration (empty = open)
	RetentionDays             int
	ContainerLogRetentionDays int
	ContainerLogMaxGB         int
	CommandResultsMaxGB       int
	DBFSProbePath             string
	DBMaxGB                   int
	DBFreeLowGB               int
	DBFreeEmergencyGB         int
	MaxDevices                int
	RegistrationsPerDay       int
	GitHubRepo                string
	AdminPasswordHash         string
	JWTSecret                 string
	AllowedOrigins            []string
	TLSEnabled                bool
	TLSMode                   string // "self-signed", "letsencrypt", "manual", or ""
	TLSDomain                 string // Let's Encrypt autocert domain
	TLSCertDir                string // autocert cache directory
	TLSCertFile               string // manual TLS cert file
	TLSKeyFile                string // manual TLS key file
	MTLSEnabled               bool   // enable mTLS device authentication
	SetupComplete             bool   // whether initial setup wizard has been completed

	// OIDC-001: optional "Sign in with SSO" support. Dormant unless
	// OIDCIssuerURL, OIDCClientID, and OIDCClientSecret are all non-empty
	// after trimming (policy applied in the oidc package, not here — AD-003).
	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCButtonLabel  string

	// TrustedProxies is a comma-separated list of CIDR blocks trusted to
	// supply forwarding headers (X-Forwarded-For, X-Real-IP, True-Client-IP,
	// X-Forwarded-Proto). Empty (the default) trusts nobody (AD-020).
	TrustedProxies string

	// PprofAddr is the raw (trimmed) RIOT_PPROF_ADDR value. Empty disables the
	// loopback pprof listener. It is validated by ParsePprofAddr in Start,
	// after slog is configured, so a rejection can be logged (HARDEN-002 AD-002).
	PprofAddr string

	// DisplayTZRaw (RIOT_DISPLAY_TZ) and TZRaw (TZ) are the raw, untrimmed
	// values. ResolveDisplayZone validates them in Start, after slog is
	// configured, so a rejection can be logged (TIME-12H-001 AD-001).
	DisplayTZRaw string
	TZRaw        string
}

// Where the effective display zone came from.
const (
	DisplayZoneSourceEnv     = "RIOT_DISPLAY_TZ"
	DisplayZoneSourceTZ      = "TZ"
	DisplayZoneSourceDefault = "default"
)

// maxDisplayZoneLen caps a zone name, and the logged copy of a rejected one.
const maxDisplayZoneLen = 64

// displayZoneShape is the FR-002 name shape: 1 to 3 "/"-separated segments,
// each starting with a letter. It admits no ".", ":", "\" or leading "/", so a
// path, "../x" or the POSIX ":/etc/localtime" form never reaches LoadLocation.
var displayZoneShape = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z][A-Za-z0-9_+-]*){0,2}$`)

// DisplayZone is the zone server-written text and the dashboard's Server zone
// use. Name is Location.String(): the trimmed source value, or "UTC".
type DisplayZone struct {
	Location *time.Location
	Name     string
	Source   string
}

// ResolveDisplayZone picks the display zone from RIOT_DISPLAY_TZ, then TZ, then
// UTC, using the first valid value (FR-001). A blank source is unset and silent;
// a set but invalid source logs one warning and the next source is tried. It
// never fails and never assigns time.Local (FR-006, FR-007).
//
// Start calls it exactly once per process. It is deliberately not reachable from
// setupRouter, which applyTLSAndRestart re-runs, so the log lines stay bounded.
func ResolveDisplayZone(displayTZ, tz string, log *slog.Logger) DisplayZone {
	sources := []struct{ name, raw string }{
		{DisplayZoneSourceEnv, displayTZ},
		{DisplayZoneSourceTZ, tz},
	}
	zone := DisplayZone{Location: time.UTC, Name: "UTC", Source: DisplayZoneSourceDefault}
	for _, src := range sources {
		v := strings.TrimSpace(src.raw)
		if v == "" {
			continue
		}
		// The shape check runs first so LoadLocation only ever sees Area/City
		// names. Its error is never logged: it echoes the raw input.
		if validDisplayZoneName(v) {
			if loc, err := time.LoadLocation(v); err == nil {
				zone = DisplayZone{Location: loc, Name: v, Source: src.name}
				break
			}
		}
		log.Warn("ignoring invalid display time zone", "source", src.name, "value", sanitizeZoneValue(v))
	}
	log.Info("display time zone", "zone", zone.Name, "source", zone.Source)
	return zone
}

// validDisplayZoneName checks the shape of a trimmed zone name only; whether
// the zone database knows it is LoadLocation's answer. "Local" is refused
// because LoadLocation would return the process zone for it (FR-007).
func validDisplayZoneName(s string) bool {
	return len(s) <= maxDisplayZoneLen && s != "Local" && displayZoneShape.MatchString(s)
}

// sanitizeZoneValue makes a rejected value safe to log: valid UTF-8, no Cc
// (controls) or Cf (bidi overrides and isolates, zero-width, BOM) runes, and at
// most 64 runes. Cf runes could make Settings → Logs show a different value
// than was logged (SEC-002).
func sanitizeZoneValue(s string) string {
	s = strings.ToValidUTF8(s, "�")
	out := make([]rune, 0, maxDisplayZoneLen)
	for _, r := range s {
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		out = append(out, r)
		if len(out) == maxDisplayZoneLen {
			break
		}
	}
	return string(out)
}

func LoadConfig() *Config {
	cfg := &Config{
		Port:                      7331,
		DBUrl:                     "postgres://riot:riot@localhost:5432/riot?sslmode=disable",
		RetentionDays:             30,
		ContainerLogRetentionDays: 7,
		ContainerLogMaxGB:         16,
		CommandResultsMaxGB:       4,
		MaxDevices:                500,
		RegistrationsPerDay:       100,
		GitHubRepo:                "rbretschneider/rIOt",
	}

	if v := os.Getenv("RIOT_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Port = p
		}
	}
	if v := os.Getenv("RIOT_DB_URL"); v != "" {
		cfg.DBUrl = v
	}
	if v := os.Getenv("RIOT_API_KEY"); v != "" {
		cfg.RegistrationKey = v
	}
	if v := os.Getenv("RIOT_RETENTION_DAYS"); v != "" {
		if d, err := strconv.Atoi(v); err == nil {
			cfg.RetentionDays = d
		}
	}
	cfg.ContainerLogRetentionDays = parseContainerLogRetention(os.Getenv("RIOT_CONTAINER_LOG_RETENTION_DAYS"))
	cfg.ContainerLogMaxGB = positiveEnv("RIOT_CONTAINER_LOG_MAX_GB", 16)
	cfg.CommandResultsMaxGB = positiveEnv("RIOT_COMMAND_RESULTS_MAX_GB", 4)
	cfg.DBFSProbePath = os.Getenv("RIOT_DB_FS_PROBE_PATH")
	cfg.DBMaxGB = positiveEnv("RIOT_DB_MAX_GB", 0)
	cfg.DBFreeLowGB = positiveEnv("RIOT_DB_FREE_LOW_GB", 0)
	cfg.DBFreeEmergencyGB = positiveEnv("RIOT_DB_FREE_EMERGENCY_GB", 0)
	cfg.MaxDevices = positiveEnv("RIOT_MAX_DEVICES", 500)
	cfg.RegistrationsPerDay = positiveEnv("RIOT_REGISTRATIONS_PER_DAY", 100)
	if v := os.Getenv("RIOT_GITHUB_REPO"); v != "" {
		cfg.GitHubRepo = v
	}

	// Admin password — bcrypt hash computed at load time
	if v := os.Getenv("RIOT_ADMIN_PASSWORD"); v != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(v), bcrypt.DefaultCost)
		if err != nil {
			slog.Error("failed to hash admin password", "error", err.Error())
		} else {
			cfg.AdminPasswordHash = string(hash)
		}
	}

	// JWT secret — auto-generated if not provided
	if v := os.Getenv("RIOT_JWT_SECRET"); v != "" {
		cfg.JWTSecret = v
	} else {
		b := make([]byte, 32)
		rand.Read(b)
		cfg.JWTSecret = hex.EncodeToString(b)
		slog.Info("generated random JWT secret (set RIOT_JWT_SECRET for stable sessions across restarts)")
	}

	// TLS
	if v := os.Getenv("RIOT_TLS_ENABLED"); v == "true" || v == "1" {
		cfg.TLSEnabled = true
	}
	if v := os.Getenv("RIOT_TLS_DOMAIN"); v != "" {
		cfg.TLSDomain = v
		cfg.TLSEnabled = true // domain implies TLS
	}
	if v := os.Getenv("RIOT_TLS_CERT_DIR"); v != "" {
		cfg.TLSCertDir = v
	}
	if v := os.Getenv("RIOT_TLS_CERT_FILE"); v != "" {
		cfg.TLSCertFile = v
	}
	if v := os.Getenv("RIOT_TLS_KEY_FILE"); v != "" {
		cfg.TLSKeyFile = v
	}

	// mTLS
	if v := os.Getenv("RIOT_MTLS_ENABLED"); v == "true" || v == "1" {
		cfg.MTLSEnabled = true
	}

	// Allowed CORS origins
	if v := os.Getenv("RIOT_ALLOWED_ORIGINS"); v != "" {
		for _, o := range strings.Split(v, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
			}
		}
	}

	// OIDC-001: read verbatim (trimmed), no validation here — policy is
	// applied by the oidc package (AD-003). No new required variable (FR-008).
	cfg.OIDCIssuerURL = strings.TrimSpace(os.Getenv("RIOT_OIDC_ISSUER_URL"))
	cfg.OIDCClientID = strings.TrimSpace(os.Getenv("RIOT_OIDC_CLIENT_ID"))
	cfg.OIDCClientSecret = strings.TrimSpace(os.Getenv("RIOT_OIDC_CLIENT_SECRET"))
	cfg.OIDCButtonLabel = strings.TrimSpace(os.Getenv("RIOT_OIDC_BUTTON_LABEL"))
	cfg.TrustedProxies = strings.TrimSpace(os.Getenv("RIOT_TRUSTED_PROXIES"))
	cfg.PprofAddr = strings.TrimSpace(os.Getenv("RIOT_PPROF_ADDR"))
	cfg.DisplayTZRaw = os.Getenv("RIOT_DISPLAY_TZ")
	cfg.TZRaw = os.Getenv("TZ")

	return cfg
}

package models

// AutomationConfig holds server-wide automation scheduling settings.
type AutomationConfig struct {
	OSPatch      MaintenanceWindow `json:"os_patch"`
	DockerUpdate MaintenanceWindow `json:"docker_update"`
}

// MaintenanceWindow defines when an automated action is allowed to run.
type MaintenanceWindow struct {
	Mode            string `json:"mode"`             // "anytime", "window", "disabled"
	StartTime       string `json:"start_time"`       // "HH:MM" in UTC (e.g. "23:00")
	EndTime         string `json:"end_time"`         // "HH:MM" in UTC (e.g. "05:00")
	CooldownMinutes int    `json:"cooldown_minutes"` // minimum minutes between triggers per target
	StaggerSeconds  int    `json:"stagger_seconds"`  // minimum seconds between any two dispatches on a single device
}

// Default stagger between per-target auto-update dispatches on the same device.
// Sized so that ~60 containers complete in ~10 hours and stay well under the
// anonymous Docker Hub limit of 100 pulls per 6 hours per IP.
const DefaultDockerStaggerSeconds = 600

// DefaultAutomationConfig returns the default config. Both Docker updates and
// OS patches default to "disabled" — auto-updates must be explicitly configured
// with a maintenance window or set to "anytime". This prevents surprise container
// recreations (and broken dashboard sessions) when the agent detects a new image.
func DefaultAutomationConfig() AutomationConfig {
	return AutomationConfig{
		OSPatch: MaintenanceWindow{
			Mode:            "disabled",
			StartTime:       "23:00",
			EndTime:         "05:00",
			CooldownMinutes: 360, // 6 hours
			StaggerSeconds:  0,   // OS patches are per-device, no staggering needed
		},
		DockerUpdate: MaintenanceWindow{
			Mode:            "disabled",
			StartTime:       "23:00",
			EndTime:         "05:00",
			CooldownMinutes: 10080, // 1 week — avoids re-pulling the same image on every run
			StaggerSeconds:  DefaultDockerStaggerSeconds,
		},
	}
}

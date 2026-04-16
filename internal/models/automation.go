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
	CooldownMinutes int    `json:"cooldown_minutes"` // minimum minutes between triggers
}

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
		},
		DockerUpdate: MaintenanceWindow{
			Mode:            "disabled",
			StartTime:       "23:00",
			EndTime:         "05:00",
			CooldownMinutes: 30,
		},
	}
}

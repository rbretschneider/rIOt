package models

// Docker capability tokens describe additive protocol support; they never grant authorization.
const (
	DockerCapLogCaps          = "log_caps_v1"
	DockerCapMeta             = "docker_meta_v1"
	DockerCapBulkResults      = "bulk_results_v1"
	DockerCapContainerLocks   = "container_locks_v1"
	DockerCapLogsLive         = "logs_live_v1"
	DockerCapInspect          = "inspect_v1"
	DockerCapCommandSteps     = "cmd_steps_v1"
	DockerCapEventAttribution = "event_attribution_v1"
	DockerCapStackActions     = "stack_actions_v1"
	DockerCapComposeRead      = "compose_read_v1"
	DockerCapInventory        = "inventory_v1"
	DockerCapPrune            = "prune_v1"
)

import type { Command, Event, SecurityFinding } from '../../types/models'
import type { BadgeTone } from './Badge'

// The single enum-to-label map. Raw severity, command-status, command-action
// and event-type values must never reach the screen: render them through
// enumLabel(). Each union is derived from a runtime array so tests can iterate
// every value, and each label table is Record<Union, string>, so a value
// missing from the map fails the type-check.

export const SEVERITIES = ['info', 'warning', 'critical', 'pass'] as const
export type Severity = (typeof SEVERITIES)[number]

// The ten canonical lifecycle states (CMD-LIFECYCLE-001), then the four legacy
// values that rows written before the lifecycle migration can still carry.
export const COMMAND_STATUSES = [
  'queued',
  'dispatched',
  'acknowledged',
  'running',
  'verifying',
  'succeeded',
  'failed',
  'timed_out',
  'cancelled',
  'expired',
  'pending',
  'sent',
  'success',
  'error',
] as const
export type CommandStatus = (typeof COMMAND_STATUSES)[number]

// Every action dispatched by internal/agent/commands.go.
export const COMMAND_ACTIONS = [
  'os_update',
  'agent_update',
  'agent_uninstall',
  'reboot',
  'shutdown',
  'fetch_logs',
  'enable_auto_updates',
  'run_device_probe',
  'docker_start',
  'docker_stop',
  'docker_restart',
  'docker_update',
  'docker_bulk_update',
  'docker_check_updates',
] as const
export type CommandAction = (typeof COMMAND_ACTIONS)[number]

// Every event type in internal/models/events.go and probes.go. Event.type is a
// plain string on the wire; unknown values fall back to humanize().
export const EVENT_TYPES = [
  'device_online',
  'device_offline',
  'disk_high',
  'mem_high',
  'update_available',
  'container_started',
  'container_stopped',
  'container_died',
  'container_oom_killed',
  'container_created',
  'container_destroyed',
  'container_paused',
  'container_unpaused',
  'container_update_started',
  'container_update_completed',
  'container_update_failed',
  'service_stopped',
  'service_failed',
  'process_missing',
  'nic_down',
  'command_sent',
  'command_completed',
  'agent_update_available',
  'agent_update_started',
  'agent_update_completed',
  'agent_update_failed',
  'log_errors',
  'container_high_cpu',
  'container_high_mem',
  'container_cpu_over_limit',
  'ups_on_battery',
  'ups_low_battery',
  'ups_restored',
  'cert_expiring',
  'cert_expired',
  'usb_disconnected',
  'disk_smart_failing',
  'disk_smart_temp',
  'gpu_temp',
  'gpu_metric',
  'nginx_5xx_high',
  'nginx_4xx_high',
  'nginx_request_high',
  'auth_failure',
  'detector_initialized',
  'reboot_required',
  'probe_down',
  'probe_recovered',
  'tls_expiring',
] as const
export type EventType = (typeof EVENT_TYPES)[number]

export type LabelCategory = 'severity' | 'commandStatus' | 'commandAction' | 'eventType'

export const LABELS: {
  severity: Record<Severity, string>
  commandStatus: Record<CommandStatus, string>
  commandAction: Record<CommandAction, string>
  eventType: Record<EventType, string>
} = {
  severity: {
    info: 'Info',
    warning: 'Warning',
    critical: 'Critical',
    pass: 'Pass',
  },
  commandStatus: {
    queued: 'Queued',
    dispatched: 'Sent to agent',
    acknowledged: 'Acknowledged',
    running: 'Running',
    verifying: 'Verifying',
    succeeded: 'Succeeded',
    failed: 'Failed',
    timed_out: 'Timed out',
    cancelled: 'Cancelled',
    expired: 'Expired',
    pending: 'Pending',
    sent: 'Sent',
    success: 'Success',
    error: 'Error',
  },
  commandAction: {
    os_update: 'OS update',
    agent_update: 'Agent update',
    agent_uninstall: 'Agent uninstall',
    reboot: 'Reboot',
    shutdown: 'Shutdown',
    fetch_logs: 'Fetch logs',
    enable_auto_updates: 'Enable auto-updates',
    run_device_probe: 'Run device probe',
    docker_start: 'Container start',
    docker_stop: 'Container stop',
    docker_restart: 'Container restart',
    docker_update: 'Container update',
    docker_bulk_update: 'Bulk container update',
    docker_check_updates: 'Container update check',
  },
  eventType: {
    device_online: 'Device online',
    device_offline: 'Device offline',
    disk_high: 'Disk usage high',
    mem_high: 'Memory usage high',
    update_available: 'Update available',
    container_started: 'Container started',
    container_stopped: 'Container stopped',
    container_died: 'Container died',
    container_oom_killed: 'Container OOM-killed',
    container_created: 'Container created',
    container_destroyed: 'Container destroyed',
    container_paused: 'Container paused',
    container_unpaused: 'Container unpaused',
    container_update_started: 'Container update started',
    container_update_completed: 'Container update completed',
    container_update_failed: 'Container update failed',
    service_stopped: 'Service stopped',
    service_failed: 'Service failed',
    process_missing: 'Process missing',
    nic_down: 'NIC down',
    command_sent: 'Command sent',
    command_completed: 'Command completed',
    agent_update_available: 'Agent update available',
    agent_update_started: 'Agent update started',
    agent_update_completed: 'Agent update completed',
    agent_update_failed: 'Agent update failed',
    log_errors: 'Log errors',
    container_high_cpu: 'Container CPU high',
    container_high_mem: 'Container memory high',
    container_cpu_over_limit: 'Container CPU over limit',
    ups_on_battery: 'UPS on battery',
    ups_low_battery: 'UPS battery low',
    ups_restored: 'UPS power restored',
    cert_expiring: 'Certificate expiring',
    cert_expired: 'Certificate expired',
    usb_disconnected: 'USB device disconnected',
    disk_smart_failing: 'Disk SMART failing',
    disk_smart_temp: 'Disk temperature high',
    gpu_temp: 'GPU temperature high',
    gpu_metric: 'GPU metric alert',
    nginx_5xx_high: 'Nginx 5xx rate high',
    nginx_4xx_high: 'Nginx 4xx rate high',
    nginx_request_high: 'Nginx request rate high',
    auth_failure: 'Authentication failure',
    detector_initialized: 'Detector initialized',
    reboot_required: 'Reboot required',
    probe_down: 'Probe down',
    probe_recovered: 'Probe recovered',
    tls_expiring: 'TLS certificate expiring',
  },
}

// Compile-time ties between the map and the wire types in types/models.ts.
type Assert<T extends true> = T
export type EventSeverityIsMapped = Assert<Event['severity'] extends Severity ? true : false>
export type FindingSeverityIsMapped = Assert<SecurityFinding['severity'] extends Severity ? true : false>
export type CommandStatusIsMapped = Assert<
  [Command['status']] extends [CommandStatus] ? ([CommandStatus] extends [Command['status']] ? true : false) : false
>

const EMPTY_LABEL = '—'

/** "some_new_value" becomes "Some new value". */
export function humanize(value: string): string {
  const spaced = value.replace(/_/g, ' ').toLowerCase()
  return spaced.charAt(0).toUpperCase() + spaced.slice(1)
}

/** Display label for an enum value. Null, undefined and '' render an em dash; unknown values are humanized. */
export function enumLabel(category: LabelCategory, value: string | null | undefined): string {
  if (value == null || value === '') return EMPTY_LABEL
  const table: Record<string, string> = LABELS[category]
  // hasOwn guard: a value like "constructor" must not resolve to Object.prototype members.
  return Object.prototype.hasOwnProperty.call(table, value) ? table[value] : humanize(value)
}

/** Default badge tone for an enum value. */
export function toneFor(category: LabelCategory, value: string | null | undefined): BadgeTone {
  if (category === 'severity') {
    if (value === 'critical') return 'danger'
    if (value === 'warning') return 'warn'
    if (value === 'info') return 'info'
    if (value === 'pass') return 'success'
  }
  if (category === 'commandStatus') {
    if (value === 'success' || value === 'succeeded') return 'success'
    if (value === 'error' || value === 'failed' || value === 'timed_out') return 'danger'
    if (value === 'sent' || value === 'queued' || value === 'dispatched' || value === 'acknowledged') return 'warn'
    if (value === 'running' || value === 'verifying') return 'info'
  }
  return 'neutral'
}

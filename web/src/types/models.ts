export interface Device {
  id: string
  short_id: string
  hostname: string
  arch: string
  agent_version?: string
  primary_ip?: string
  status: 'online' | 'offline' | 'warning'
  agent_connected?: boolean
  location: string
  tags: string[]
  docker_available: boolean
  docker_container_count: number
  docker_auto_update_container_count: number
  auto_patch: boolean
  has_auto_update: boolean
  hardware_profile?: HardwareProfile
  last_heartbeat?: string
  last_telemetry?: string
  created_at: string
  updated_at: string
}

export interface HardwareProfile {
  cpu_model: string
  cpu_cores: number
  cpu_threads: number
  total_ram_mb: number
  board_model?: string
  serial_number?: string
  bios_version?: string
  bios_date?: string
  virtualization?: string
}

export interface HeartbeatData {
  uptime: number
  cpu_percent: number
  mem_percent: number
  load_avg_1m: number
  disk_root_percent: number
  disk_read_bytes_sec?: number
  disk_write_bytes_sec?: number
  disk_io_percent?: number
  net_rx_bytes_sec?: number
  net_tx_bytes_sec?: number
  agent_version?: string
  log_errors?: number
}

export interface LogEntry {
  timestamp: string
  priority: number
  unit: string
  message: string
}

export interface Heartbeat {
  id: number
  device_id: string
  timestamp: string
  data: HeartbeatData
}

export interface TelemetrySnapshot {
  id: number
  device_id: string
  timestamp: string
  data: FullTelemetryData
}

export interface FullTelemetryData {
  trimmed?: boolean
  dropped_sections?: string[]
  system?: SystemInfo
  os?: OSInfo
  cpu?: CPUInfo
  memory?: MemoryInfo
  disks?: DiskInfo
  network?: NetworkInfo
  updates?: UpdateInfo
  services?: ServiceInfo[]
  processes?: ProcessInfo
  docker?: DockerInfo
  security?: SecurityInfo
  ups?: UPSInfo
  web_servers?: WebServerInfo
  usb?: USBInfo
  hardware?: HardwareInfo
  cron_jobs?: CronInfo
  gpu_telemetry?: GPUTelemetry
}

export interface GPUDeviceMetrics {
  index: number
  name: string
  uuid: string
  pci_bus_id: string
  temperature_c?: number
  fan_speed_percent?: number
  utilization_pct?: number
  mem_util_pct?: number
  mem_used_mib?: number
  mem_total_mib?: number
  power_draw_w?: number
  power_limit_w?: number
}

export interface GPUTelemetry {
  gpus: GPUDeviceMetrics[]
}

export interface SystemInfo {
  hostname: string
  device_uuid: string
  arch: string
  cpu_model: string
  cpu_cores: number
  cpu_threads: number
  total_ram_mb: number
  board_model?: string
  serial_number?: string
  bios_version?: string
  bios_date?: string
  virtualization?: string
}

export interface OSInfo {
  name: string
  id: string
  version: string
  codename?: string
  kernel: string
  kernel_arch: string
  init_system?: string
  boot_time: number
  uptime: number
  timezone?: string
  locale?: string
}

export interface CPUInfo {
  usage_percent: number
  per_core?: number[]
  load_avg_1m: number
  load_avg_5m: number
  load_avg_15m: number
  temperature?: number
  freq_current?: number
  freq_max?: number
}

export interface MemoryInfo {
  total_mb: number
  used_mb: number
  free_mb: number
  cached_mb: number
  buffers_mb: number
  swap_total_mb: number
  swap_used_mb: number
  usage_percent: number
}

export interface DiskInfo {
  block_devices?: BlockDevice[]
  filesystems?: Filesystem[]
}

export interface BlockDevice {
  name: string
  model?: string
  size_gb: number
  type?: string
  smart_status?: string
}

export interface Filesystem {
  mount_point: string
  device: string
  fs_type: string
  total_gb: number
  used_gb: number
  free_gb: number
  usage_percent: number
  mount_options?: string
  is_network_mount: boolean
  is_pool?: boolean  // Optional: absent from old agents; use isPoolFilesystem() for classification
}

export interface NetworkInfo {
  interfaces?: NetworkInterface[]
  default_gateway?: string
  dns_servers?: string[]
  fqdn?: string
}

export interface NetworkInterface {
  name: string
  mac?: string
  ipv4?: string[]
  ipv6?: string[]
  state: string
  speed_mbps?: number
  bytes_sent: number
  bytes_recv: number
}

export interface UpdateInfo {
  package_manager?: string
  total_installed: number
  pending_updates: number
  pending_security_count: number
  pending_kernel_update: boolean
  pending_kernel_version?: string
  pending_reboot_class_count?: number
  held_packages?: string[]
  hold_enforcement?: 'active' | 'no_privilege' | 'unsupported'
  reboot_required?: boolean
  reboot_required_reasons?: string[]
  updates?: PendingUpdate[]
  last_check_time?: string
  unattended_upgrades: boolean
  allow_reboot?: boolean
  // Self-reported agent settings (absent = the agent does not report them).
  allow_patching?: boolean
  hold_reboot_class?: boolean
  // PATCH-FLOW-001: apt package-list freshness. Absent reads as unknown.
  lists_freshness?: 'fresh' | 'stale' | 'unknown'
  // The agent understands os_update reboot_if_required (only true counts).
  supports_reboot_if_required?: boolean
  // EXTRA-UPD-001: platform updaters outside the package manager (at most 8). Absent from older agents.
  extra_updaters?: ExtraUpdater[]
}

export type ExtraUpdaterStatus = 'update_available' | 'up_to_date' | 'unknown'

export interface ExtraUpdaterNotice {
  code: string
  branch?: string
  owner?: string
}

export interface ExtraUpdater {
  id: string
  status: ExtraUpdaterStatus
  unknown_reason?: string
  installed_version?: string
  available_version?: string
  pending_count: number
  pending_items?: string[]
  // Whether the agent would apply the updater: 'enabled' or the reason it would not.
  readiness?: string
  notices?: ExtraUpdaterNotice[]
}

export interface PendingUpdate {
  name: string
  current_ver: string
  new_ver: string
  is_security: boolean
  class?: 'gpu_driver' | 'kernel'
}

export interface ServiceInfo {
  name: string
  state: string
  enabled: boolean
  pid?: number
  memory_mb?: number
}

export interface ProcessInfo {
  top_by_cpu?: ProcessEntry[]
  top_by_memory?: ProcessEntry[]
}

export interface ProcessEntry {
  pid: number
  name: string
  cpu_percent: number
  mem_percent: number
  mem_mb: number
  user: string
  command?: string
}

export interface DockerInfo {
  available: boolean
  version?: string
  api_version?: string
  total_containers: number
  running: number
  stopped: number
  paused: number
  images_total: number
  storage_driver?: string
  docker_root_dir?: string
  containers?: ContainerInfo[]
}

export interface ContainerInfo {
  started_at?: string
  finished_at?: string
  exit_code?: number
  mem_limit_configured?: boolean
  net_rx_bytes?: number
  net_tx_bytes?: number
  blk_read_bytes?: number
  blk_write_bytes?: number
  net_rx_rate?: number
  net_tx_rate?: number
  blk_read_rate?: number
  blk_write_rate?: number
  id: string
  short_id: string
  name: string
  image: string
  state: string
  status: string
  created: number
  ports?: PortMapping[]
  labels?: Record<string, string>
  cpu_percent: number
  mem_usage: number
  mem_limit: number
  cpu_limit?: number
  restart_policy?: string
  restart_count?: number
  mounts?: MountInfo[]
  networks?: NetworkAttach[]
  env?: EnvVar[]
  health_status?: string
  repo_url?: string
  riot?: RiotLabels
  update_available?: boolean | null
  // CMD-LIFECYCLE-001 (AD-016): per-container freshness verdict. Absent when the
  // agent predates the verdict; fall back to update_available in that case.
  image_id?: string
  update_status?: UpdateStatus
  update_reason?: UpdateReason
  network_mode?: string
  uses_gpu?: boolean
}

export type UpdateStatus = 'up_to_date' | 'update_available' | 'pinned' | 'check_failed'
export type UpdateReason = 'registry_newer' | 'pulled_not_recreated'

export interface PortMapping {
  container_port: string
  host_port?: string
  protocol: string
  host_ip?: string
}

export interface MountInfo {
  type: string
  source: string
  destination: string
  read_only: boolean
}

export interface NetworkAttach {
  name: string
  ip_address?: string
  gateway?: string
  mac_addr?: string
}

export interface EnvVar {
  key: string
  value: string
}

export interface RiotLabels {
  role?: string
  group?: string
  name?: string
  icon?: string
  description?: string
  url?: string
  priority: number
  hide?: boolean
  tags?: string[]
}

export interface ContainerLogEntry {
  id?: number
  device_id?: string
  container_id: string
  container_name: string
  timestamp: string
  stream: 'stdout' | 'stderr'
  line: string
}

export interface ContainerMetric {
  net_rx_rate?: number
  net_tx_rate?: number
  blk_read_rate?: number
  blk_write_rate?: number
  id?: number
  device_id: string
  container_name: string
  container_id: string
  timestamp: string
  cpu_percent: number
  mem_usage: number
  mem_limit: number
  cpu_limit?: number
}

export interface DockerEvent {
  container_id: string
  container_name: string
  action: string
  image?: string
}

export interface UPSInfo {
  name: string
  status: string
  on_battery: boolean
  low_battery: boolean
  battery_charge?: number
  battery_runtime?: number
  input_voltage?: number
  output_voltage?: number
  load?: number
  model?: string
  manufacturer?: string
}

export interface SecurityInfo {
  selinux?: string
  apparmor?: string
  firewall_status?: string
  failed_logins_24h: number
  failed_logins_interval?: number
  logged_in_users: number
  open_ports?: number[]
}

export interface WebServerInfo {
  servers?: ProxyServer[]
}

export interface ProxyServer {
  name: string
  version?: string
  status: string
  pid?: number
  config_path?: string
  config_valid?: boolean
  config_error?: string
  sites?: ProxySite[]
  certs?: ProxyCert[]
  upstreams?: ProxyUpstream[]
  security_config?: ProxySecurityCfg
}

export interface ProxySite {
  server_names?: string[]
  listen?: string[]
  root?: string
  proxy_pass?: string
  ssl_cert?: string
  enabled: boolean
  config_file?: string
}

export interface ProxyCert {
  file_path: string
  subject?: string
  issuer?: string
  sans?: string[]
  not_before?: string
  not_after?: string
  days_left: number
  key_type?: string
  is_ca?: boolean
  fingerprint?: string
}

export interface ProxyUpstream {
  name: string
  servers?: UpstreamServer[]
}

export interface UpstreamServer {
  address: string
  weight?: number
  backup?: boolean
  down?: boolean
}

export interface ProxySecurityCfg {
  rate_limiting?: RateLimitRule[]
  access_controls?: AccessRule[]
  security_headers?: Record<string, string>
  allowed_methods?: string[]
  cors_origins?: string[]
}

export interface RateLimitRule {
  zone: string
  rate: string
  burst?: number
}

export interface AccessRule {
  directive: string
  value: string
  location?: string
}

export interface USBInfo {
  devices?: USBDevice[]
}

export interface USBDevice {
  bus: string
  device: string
  vendor_id: string
  product_id: string
  vendor?: string
  product?: string
  serial?: string
  description: string
  device_class?: string
  speed_mbps?: number
  sys_path?: string
}

export interface HardwareInfo {
  pci_devices?: PCIDevice[]
  disk_drives?: DiskDrive[]
  serial_ports?: SerialPort[]
  gpus?: GPUInfo[]
}

export interface PCIDevice {
  slot: string
  vendor_id: string
  device_id: string
  vendor?: string
  device?: string
  description: string
  class_code?: string
  class_name?: string
  driver?: string
  subsys_vendor_id?: string
  subsys_device_id?: string
  numa_node?: string
  irq?: string
}

export interface DiskDrive {
  name: string
  model?: string
  serial?: string
  rev?: string
  size_bytes: number
  size_gb: number
  type?: string
  transport?: string
  removable?: boolean
  scheduler?: string
  smart_health?: string
  smart_temp?: number
  smart_power_on_hours?: number
  smart_reallocated?: number
  smart_pending_sector?: number
  smart_available?: boolean
}

export interface SerialPort {
  name: string
  path: string
  type?: string
  driver?: string
}

export interface GPUInfo {
  vendor?: string
  model?: string
  pci_slot?: string
  driver?: string
  vram_mb?: number
  description: string
}

export interface CronInfo {
  jobs?: CronJob[]
  timers?: CronTimer[]
}

export interface CronJob {
  user: string
  schedule: string
  command: string
  source: string
  enabled: boolean
  next_run?: string
  last_run?: string
  task_name?: string
}

export interface CronTimer {
  name: string
  calendar: string
  next_run?: string
  last_run?: string
  unit: string
  enabled: boolean
}

export interface DeviceProbe {
  id: number
  name: string
  device_id: string
  type: string
  enabled: boolean
  config: Record<string, unknown>
  assertions: ProbeAssertion[]
  interval_seconds: number
  timeout_seconds: number
  created_at: string
  updated_at: string
}

export interface ProbeAssertion {
  field: string
  operator: string
  value: string
}

export interface DeviceProbeResult {
  id: number
  probe_id: number
  device_id: string
  success: boolean
  latency_ms: number
  output: Record<string, unknown>
  failed_assertions?: ProbeAssertion[]
  error_msg?: string
  created_at: string
}

export interface DeviceProbeWithResult extends DeviceProbe {
  latest_result?: DeviceProbeResult
  success_rate?: number
  total_checks: number
}

export interface DeviceProbeWithResultEnriched extends DeviceProbeWithResult {
  device_hostname: string
}

// Security Score
export interface SecurityFinding {
  id: string
  category: string
  severity: 'critical' | 'warning' | 'info' | 'pass'
  title: string
  description: string
  remediation: string
  ref_url?: string
  weight: number
  passed: boolean
}

export interface SecurityCategoryScore {
  category: string
  label: string
  score: number
  max_score: number
  findings: SecurityFinding[]
}

export interface SecurityScoreResult {
  overall_score: number
  max_score: number
  grade: string
  categories: SecurityCategoryScore[]
  evaluated_at: string
}

export interface Event {
  id: number
  device_id: string
  type: string
  severity: 'info' | 'warning' | 'critical'
  message: string
  container_id?: string
  container_name?: string
  created_at: string
  acknowledged_at?: string
  // EXPECT-002: the command an event is linked to and how the link was made
  // (`expected`, `escalation`, `outcome`, `inferred`, `agent`). Absent on unlinked events.
  command_id?: string
  command_attr?: string
}

export interface FleetSummary {
  total_devices: number
  online_count: number
  offline_count: number
  warning_count: number
  recent_events: number
}

export interface DeviceDetailResponse {
  device: Device
  latest_telemetry?: TelemetrySnapshot
  agent_connected?: boolean
}

export interface UpdateInfo {
  current_version: string
  latest_version: string
  update_available: boolean
  release_url?: string
  published_at?: string
  server_host_device_id?: string
}

// EXPECT-001 (ADD 6.1): server-computed activity per device. At most one entry
// per device; the server owns selection (host or agent over docker), the web
// only renders. Times are RFC 3339 strings.
export type ActivityKind = 'host' | 'agent' | 'docker' | 'powered_off' | 'escalated'
export type ActivityPhase = 'queued' | 'in_progress' | 'return' | 'settling' | 'powered_off' | 'escalated'
export type ActivityEscalationReason = 'failed' | 'timed_out' | 'no_result' | 'not_back'

export interface ActivityEscalation {
  reason: ActivityEscalationReason
  bound: string
  at: string
}

export interface DeviceActivity {
  device_id: string
  kind: ActivityKind
  phase: ActivityPhase
  action: string
  command_id: string
  actor_phrase: string
  started_at: string
  back_by: string | null
  waiting_dpkg_lock: boolean
  progress: number | null
  agent_from_version: string
  // EXPECT-002: the version an agent self-update goes to; '' for command windows.
  // Optional so an older server (and older fixtures) still typecheck.
  agent_to_version?: string
  target: string
  docker_actions: number
  covers_offline: boolean
  device_online: boolean
  escalation: ActivityEscalation | null
  powered_off_at: string | null
}

export interface ActivityResponse {
  generated_at: string
  newest_agent_version: string
  activities: DeviceActivity[]
}

export interface WSMessage {
  type: string
  device_id?: string
  data?: unknown
  // CMD-LIFECYCLE-001 (AD-018): server-supplied timestamps on telemetry and
  // heartbeat envelopes. collected_at is agent time, clamped by the server to
  // at most received_at + 2m. Absent when talking to a pre-lifecycle server.
  collected_at?: string
  received_at?: string
}

// Alert Rules & Notifications
export interface AlertRule {
  id: number
  name: string
  enabled: boolean
  metric: string
  operator: string
  threshold: number
  target_name: string
  target_state: string
  severity: string
  include_devices: string
  exclude_devices: string
  include_containers: string
  exclude_containers: string
  cooldown_seconds: number
  notify: boolean
  template_id: string
  created_at: string
  updated_at: string
}

export interface AlertTemplate {
  id: string
  name: string
  category: string
  metric: string
  operator: string
  threshold: number
  target_state?: string
  severity: string
  cooldown_seconds: number
  needs_target_name: boolean
  description: string
}

export interface NotificationChannel {
  id: number
  name: string
  type: string
  enabled: boolean
  config: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface NotificationLog {
  id: number
  channel_id?: number
  event_id?: number
  alert_rule_id?: number
  status: string
  error_msg: string
  created_at: string
}

// Automation config
export interface MaintenanceWindow {
  mode: 'anytime' | 'window' | 'disabled'
  start_time: string
  end_time: string
  cooldown_minutes: number
  stagger_seconds: number
  frequency?: 'daily' | 'weekly' | 'monthly'
  days_of_week?: number[] // 0 = Sunday .. 6 = Saturday
  // Reboot-class (GPU driver / kernel) policy — os_patch window only.
  reboot_class?: 'off' | 'gated'
}

export interface AutomationConfig {
  os_patch: MaintenanceWindow
  docker_update: MaintenanceWindow
}

// Auto-update policies
export interface AutoUpdatePolicy {
  id: number
  device_id: string
  target: string
  is_stack: boolean
  compose_work_dir: string
  enabled: boolean
  last_triggered_at?: string
  created_at: string
}

// Commands

/** Canonical command states (CMD-LIFECYCLE-001 FR-071). */
export type CommandState =
  | 'queued'
  | 'dispatched'
  | 'acknowledged'
  | 'running'
  | 'verifying'
  | 'succeeded'
  | 'failed'
  | 'timed_out'
  | 'cancelled'
  | 'expired'

/** Values a pre-lifecycle server may still return (FR-083). */
export type LegacyCommandStatus = 'pending' | 'sent' | 'success' | 'error'

export type CommandOrigin = 'user' | 'auto_update' | 'auto_patch' | 'agent_self_update'

export interface Command {
  target_results?: {container:string;outcome:string;message?:string;step?:string}[]
  actor_kind?: string
  actor_auth?: string
  actor_name?: string
  steps?: {seq:number;name:string;started_at:string;finished_at?:string;outcome?:string}[]
  id: string
  device_id: string
  action: string
  params: Record<string, unknown>
  /** Canonical from a lifecycle server; may be legacy from an older one. Use normalizeStatus(). */
  status: CommandState | LegacyCommandStatus
  result_msg: string
  duration_ms: number | null
  exit_code: number | null
  created_at: string
  updated_at: string
  // Lifecycle fields (ADD 6.1). Optional so a pre-lifecycle server still typechecks.
  expires_at?: string
  exec_deadline_s?: number
  deadline_at?: string
  dispatched_at?: string
  acknowledged_at?: string
  started_at?: string
  verifying_at?: string
  finished_at?: string
  step?: string
  progress?: number | null
  failure_step?: string
  stderr_tail?: string
  origin?: CommandOrigin
  policy_id?: number | null
  target_key?: string
  target_keys?: string[]
}

/**
 * EXPECT-002 (ADD 6.3): the events linked to one command, server-authored events
 * kept apart from agent-sourced ones so a flood of agent events cannot hide the
 * server's account. Go nil slices may arrive as null.
 */
export interface CommandEventsResponse {
  server_events: Event[] | null
  server_events_omitted: number
  agent_events: Event[] | null
  agent_events_omitted: number
}

/** WS `command_update` payload (ADD 6.5), one per accepted transition. */
export interface CommandUpdate {
  command_id: string
  device_id: string
  action: string
  target_key: string
  target_keys: string[]
  status: CommandState | LegacyCommandStatus
  step: string
  progress: number | null
  message: string
  failure_step: string
  origin: CommandOrigin
  updated_at: string
}

/** One skipped target of a bulk update (ADD 6.2). */
export interface BulkConflict {
  container_id: string
  target_key: string
  existing_command_id: string
}

/** 201 body of the bulk docker update: the created Command plus skipped targets. */
export type BulkDockerUpdateResult = Command & { conflicts?: BulkConflict[] }

/** 409 body shared by single and bulk create (ADD 6.2). */
export interface CommandConflictBody {
  error: string
  code: 'conflict'
  existing_command_id: string
  existing_status?: CommandState | LegacyCommandStatus
  target_key?: string
  conflicts?: BulkConflict[]
}

export interface CommandOutput {
  id: number
  command_id: string
  stream: string
  content: string
  created_at: string
}

export interface CommandResult {
  command_id: string
  status: string
  message: string
}

// Probes
export interface Probe {
  id: number
  name: string
  type: string
  enabled: boolean
  config: Record<string, unknown>
  interval_seconds: number
  timeout_seconds: number
  created_at: string
  updated_at: string
}

export interface ProbeWithResult extends Probe {
  latest_result?: ProbeResult
  success_rate?: number
  total_checks: number
}

export interface ProbeResult {
  id: number
  probe_id: number
  success: boolean
  latency_ms: number
  status_code?: number
  error_msg: string
  metadata: Record<string, unknown>
  created_at: string
}

// --- Fleet Dashboard (FLEET-DASH) ---

/** Per-heartbeat shape in FleetHeartbeatsResponse.devices */
export interface FleetHeartbeat {
  device_id: string
  timestamp: string
  data: HeartbeatData
}

/** Response from GET /api/v1/fleet/heartbeats?window=60m */
export interface FleetHeartbeatsResponse {
  since: string
  until: string
  /** Map of deviceId → heartbeat array (chronological order) */
  devices: Record<string, FleetHeartbeat[]>
  /** Device IDs whose latest telemetry contains GPU data */
  devices_with_gpu: string[]
}

/**
 * Per-container row from GET /api/v1/fleet/containers.
 * Strict 12-field allowlist per AD-012 — no Labels map.
 */
export interface FleetContainerRow {
  device_id: string
  hostname: string
  container_id: string
  container_name: string
  image: string
  stack: string
  state: string
  cpu_percent: number
  mem_usage: number
  mem_limit: number
  restart_count: number
  update_available?: boolean | null
  update_status?: UpdateStatus
  /** Absent when the server could not load the policies. */
  auto_update_policy?: FleetAutoUpdatePolicy
}

/** Auto-update state of one container row, computed with the dispatcher's rule. */
export interface FleetAutoUpdatePolicy {
  /** The exact policy target for this container; "" when it cannot be targeted. */
  target: string
  /** An enabled container policy exists under `target`. */
  enabled: boolean
  /** The Compose project whose enabled stack policy handles this container. */
  covered_by?: string
}

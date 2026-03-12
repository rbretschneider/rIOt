export interface Device {
  id: string
  short_id: string
  hostname: string
  arch: string
  agent_version?: string
  primary_ip?: string
  status: 'online' | 'offline' | 'warning'
  agent_connected?: boolean
  tags: string[]
  docker_available: boolean
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
  updates?: PendingUpdate[]
  last_check_time?: string
  unattended_upgrades: boolean
}

export interface PendingUpdate {
  name: string
  current_ver: string
  new_ver: string
  is_security: boolean
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
}

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
  group?: string
  name?: string
  icon?: string
  description?: string
  url?: string
  priority: number
  hide?: boolean
  tags?: string[]
}

export interface ContainerMetric {
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

// Security Score
export interface SecurityFinding {
  id: string
  category: string
  severity: 'critical' | 'warning' | 'info' | 'pass'
  title: string
  description: string
  remediation: string
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
  created_at: string
  acknowledged_at?: string
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

export interface WSMessage {
  type: string
  device_id?: string
  data?: unknown
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
  device_filter: string
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
export interface Command {
  id: string
  device_id: string
  action: string
  params: Record<string, unknown>
  status: string
  result_msg: string
  created_at: string
  updated_at: string
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

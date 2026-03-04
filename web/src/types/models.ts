export interface Device {
  id: string
  short_id: string
  hostname: string
  arch: string
  agent_version?: string
  primary_ip?: string
  status: 'online' | 'offline' | 'warning'
  tags: string[]
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
  version?: string
  total_containers: number
  running: number
  stopped: number
  containers?: ContainerInfo[]
}

export interface ContainerInfo {
  id: string
  name: string
  image: string
  status: string
  cpu_percent: number
  mem_mb: number
  ports?: string
}

export interface SecurityInfo {
  selinux?: string
  apparmor?: string
  firewall_status?: string
  failed_logins_24h: number
  logged_in_users: number
  open_ports?: number[]
}

export interface Event {
  id: number
  device_id: string
  type: string
  severity: 'info' | 'warning' | 'critical'
  message: string
  created_at: string
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
}

export interface UpdateInfo {
  current_version: string
  latest_version: string
  update_available: boolean
  release_url?: string
  published_at?: string
}

export interface WSMessage {
  type: string
  device_id?: string
  data?: unknown
}

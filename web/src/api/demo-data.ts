/**
 * Demo data factories for the static GitHub Pages demo.
 * All timestamps are computed relative to Date.now() so the data always looks fresh.
 */
import type {
  Device, DeviceDetailResponse, TelemetrySnapshot, FullTelemetryData,
  ContainerInfo, Event, FleetSummary, ProbeWithResult, ProbeResult,
  AlertRule, AlertTemplate, NotificationChannel, NotificationLog,
  Command, UPSInfo,
} from '../types/models'
import type { DevicePatchInfo } from './client'

// ── Helpers ──────────────────────────────────────────────────────────────────

const ago = (ms: number) => new Date(Date.now() - ms).toISOString()
const MIN = 60_000
const HOUR = 3_600_000
const DAY = 86_400_000

function uuid(seed: string) {
  // Deterministic fake UUID from seed
  const h = Array.from(seed).reduce((a, c) => ((a << 5) - a + c.charCodeAt(0)) | 0, 0)
  const hex = Math.abs(h).toString(16).padStart(8, '0')
  return `${hex}-${hex.slice(0, 4)}-4${hex.slice(1, 4)}-a${hex.slice(1, 4)}-${hex}${hex.slice(0, 4)}`
}

// ── Devices ──────────────────────────────────────────────────────────────────

const deviceDefs = [
  {
    hostname: 'proxmox-01', arch: 'amd64', status: 'online' as const,
    ip: '10.0.10.5', version: '0.9.2', tags: ['hypervisor', 'docker-host'],
    hw: { cpu_model: 'Intel Xeon E-2288G', cpu_cores: 8, cpu_threads: 16, total_ram_mb: 65536, board_model: 'Supermicro X11SCH-F' },
    os: { name: 'Debian GNU/Linux', id: 'debian', version: '12', kernel: '6.1.0-28-amd64' },
  },
  {
    hostname: 'nas-synology', arch: 'amd64', status: 'online' as const,
    ip: '10.0.10.10', version: '0.9.2', tags: ['storage', 'backup'],
    hw: { cpu_model: 'Intel Celeron J4125', cpu_cores: 4, cpu_threads: 4, total_ram_mb: 8192, board_model: 'Synology DS920+' },
    os: { name: 'DSM', id: 'synology', version: '7.2.2', kernel: '4.4.302' },
  },
  {
    hostname: 'pi-dns', arch: 'arm64', status: 'online' as const,
    ip: '10.0.10.2', version: '0.9.2', tags: ['dns', 'pihole'],
    hw: { cpu_model: 'Cortex-A72', cpu_cores: 4, cpu_threads: 4, total_ram_mb: 4096, board_model: 'Raspberry Pi 4 Model B' },
    os: { name: 'Raspberry Pi OS', id: 'raspbian', version: '12', kernel: '6.6.31-v8+' },
  },
  {
    hostname: 'pi-cameras', arch: 'arm64', status: 'warning' as const,
    ip: '10.0.10.3', version: '0.9.1', tags: ['cameras', 'frigate'],
    hw: { cpu_model: 'Cortex-A76', cpu_cores: 4, cpu_threads: 4, total_ram_mb: 8192, board_model: 'Raspberry Pi 5' },
    os: { name: 'Raspberry Pi OS', id: 'raspbian', version: '12', kernel: '6.6.31-v8+' },
  },
  {
    hostname: 'k3s-worker-01', arch: 'amd64', status: 'online' as const,
    ip: '10.0.10.20', version: '0.9.2', tags: ['k3s', 'worker'],
    hw: { cpu_model: 'AMD Ryzen 5 5600G', cpu_cores: 6, cpu_threads: 12, total_ram_mb: 32768 },
    os: { name: 'Ubuntu', id: 'ubuntu', version: '24.04', kernel: '6.8.0-45-generic' },
  },
  {
    hostname: 'k3s-worker-02', arch: 'amd64', status: 'online' as const,
    ip: '10.0.10.21', version: '0.9.2', tags: ['k3s', 'worker'],
    hw: { cpu_model: 'AMD Ryzen 5 5600G', cpu_cores: 6, cpu_threads: 12, total_ram_mb: 32768 },
    os: { name: 'Ubuntu', id: 'ubuntu', version: '24.04', kernel: '6.8.0-45-generic' },
  },
  {
    hostname: 'desktop-dev', arch: 'amd64', status: 'offline' as const,
    ip: '10.0.10.50', version: '0.9.0', tags: ['workstation'],
    hw: { cpu_model: 'AMD Ryzen 9 7950X', cpu_cores: 16, cpu_threads: 32, total_ram_mb: 65536 },
    os: { name: 'Fedora Linux', id: 'fedora', version: '41', kernel: '6.11.4-301.fc41.x86_64' },
  },
  {
    hostname: 'backup-server', arch: 'amd64', status: 'warning' as const,
    ip: '10.0.10.30', version: '0.9.2', tags: ['backup', 'restic'],
    hw: { cpu_model: 'Intel Core i3-12100', cpu_cores: 4, cpu_threads: 8, total_ram_mb: 16384 },
    os: { name: 'Ubuntu', id: 'ubuntu', version: '22.04', kernel: '5.15.0-125-generic' },
  },
]

export function getDevices(): Device[] {
  return deviceDefs.map((d, i) => ({
    id: uuid(d.hostname),
    short_id: uuid(d.hostname).slice(0, 8),
    hostname: d.hostname,
    arch: d.arch,
    agent_version: d.version,
    primary_ip: d.ip,
    status: d.status,
    agent_connected: d.status !== 'offline',
    tags: d.tags,
    docker_available: ['proxmox-01', 'k3s-worker-01', 'k3s-worker-02', 'pi-cameras'].includes(d.hostname),
    hardware_profile: { ...d.hw, cpu_threads: d.hw.cpu_threads ?? d.hw.cpu_cores },
    last_heartbeat: d.status === 'offline' ? ago(3 * DAY) : ago((i + 1) * 15_000),
    last_telemetry: d.status === 'offline' ? ago(3 * DAY) : ago((i + 1) * 30_000),
    created_at: ago(90 * DAY + i * DAY),
    updated_at: d.status === 'offline' ? ago(3 * DAY) : ago((i + 1) * 30_000),
  }))
}

// ── Telemetry ────────────────────────────────────────────────────────────────

function makeUPS(hostname: string): UPSInfo | undefined {
  if (hostname === 'nas-synology') {
    return {
      name: 'ups', status: 'OL', on_battery: false, low_battery: false,
      battery_charge: 100, battery_runtime: 2340, input_voltage: 230.4,
      output_voltage: 230.4, load: 28, model: 'Back-UPS ES 700G', manufacturer: 'APC',
    }
  }
  if (hostname === 'proxmox-01') {
    return {
      name: 'eaton', status: 'OL', on_battery: false, low_battery: false,
      battery_charge: 95, battery_runtime: 1680, input_voltage: 231.2,
      output_voltage: 231.2, load: 42, model: '5E 1500i', manufacturer: 'EATON',
    }
  }
  if (hostname === 'backup-server') {
    return {
      name: 'cyberpower', status: 'OB', on_battery: true, low_battery: false,
      battery_charge: 72, battery_runtime: 1140, input_voltage: 0,
      output_voltage: 120.0, load: 34, model: 'CP1500AVRLCD', manufacturer: 'CyberPower',
    }
  }
  return undefined
}

function makeTelemetry(d: typeof deviceDefs[number]): FullTelemetryData {
  const cpuUsage = d.hostname === 'pi-cameras' ? 87 : d.hostname === 'proxmox-01' ? 34 : 15 + Math.floor(Math.random() * 20)
  const memUsed = Math.floor(d.hw.total_ram_mb * (d.hostname === 'proxmox-01' ? 0.72 : 0.45))
  return {
    system: {
      hostname: d.hostname,
      device_uuid: uuid(d.hostname),
      arch: d.arch,
      cpu_model: d.hw.cpu_model,
      cpu_cores: d.hw.cpu_cores,
      cpu_threads: d.hw.cpu_threads ?? d.hw.cpu_cores,
      total_ram_mb: d.hw.total_ram_mb,
      board_model: d.hw.board_model,
    },
    os: {
      name: d.os.name, id: d.os.id, version: d.os.version,
      kernel: d.os.kernel, kernel_arch: d.arch,
      boot_time: Math.floor((Date.now() - 14 * DAY) / 1000),
      uptime: Math.floor(14 * DAY / 1000),
      init_system: 'systemd',
    },
    cpu: {
      usage_percent: cpuUsage,
      load_avg_1m: cpuUsage / 25,
      load_avg_5m: cpuUsage / 30,
      load_avg_15m: cpuUsage / 35,
      temperature: d.hostname.startsWith('pi-') ? 58 + Math.floor(cpuUsage / 5) : undefined,
    },
    memory: {
      total_mb: d.hw.total_ram_mb,
      used_mb: memUsed,
      free_mb: d.hw.total_ram_mb - memUsed,
      cached_mb: Math.floor(memUsed * 0.3),
      buffers_mb: Math.floor(memUsed * 0.05),
      swap_total_mb: 4096,
      swap_used_mb: 128,
      usage_percent: Math.round((memUsed / d.hw.total_ram_mb) * 100),
    },
    disks: {
      block_devices: [{ name: 'sda', model: 'Samsung 870 EVO', size_gb: 500, type: 'ssd', smart_status: 'PASSED' }],
      filesystems: [
        { mount_point: '/', device: '/dev/sda1', fs_type: 'ext4', total_gb: 460, used_gb: 185, free_gb: 275, usage_percent: 40, is_network_mount: false },
        ...(d.hostname === 'nas-synology' ? [
          { mount_point: '/volume1', device: '/dev/md0', fs_type: 'btrfs', total_gb: 14400, used_gb: 8640, free_gb: 5760, usage_percent: 60, is_network_mount: false },
        ] : []),
      ],
    },
    network: {
      interfaces: [
        { name: 'eth0', mac: '00:11:22:33:44:55', ipv4: [d.ip], state: 'UP', speed_mbps: 1000, bytes_sent: 52_000_000_000, bytes_recv: 184_000_000_000 },
      ],
      default_gateway: '10.0.10.1',
      dns_servers: ['10.0.10.2', '1.1.1.1'],
    },
    services: [
      { name: 'sshd', state: 'running', enabled: true, pid: 1234, memory_mb: 8 },
      { name: 'riot-agent', state: 'running', enabled: true, pid: 5678, memory_mb: 22 },
      ...(d.hostname === 'pi-dns' ? [{ name: 'pihole-FTL', state: 'running', enabled: true, pid: 2345, memory_mb: 64 }] : []),
    ],
    processes: {
      top_by_cpu: [
        { pid: 1001, name: 'riot-agent', cpu_percent: 1.2, mem_percent: 0.3, mem_mb: 22, user: 'root' },
        { pid: 1002, name: 'node', cpu_percent: 0.8, mem_percent: 1.2, mem_mb: 96, user: 'root' },
      ],
      top_by_memory: [
        { pid: 1002, name: 'node', cpu_percent: 0.8, mem_percent: 1.2, mem_mb: 96, user: 'root' },
        { pid: 1001, name: 'riot-agent', cpu_percent: 1.2, mem_percent: 0.3, mem_mb: 22, user: 'root' },
      ],
    },
    docker: d.hostname === 'proxmox-01' ? {
      available: true,
      version: '27.4.1',
      api_version: '1.47',
      total_containers: 12,
      running: 11,
      stopped: 1,
      paused: 0,
      images_total: 14,
      storage_driver: 'overlay2',
      containers: getContainers(),
    } : {
      available: false,
      total_containers: 0, running: 0, stopped: 0, paused: 0, images_total: 0,
    },
    security: {
      firewall_status: 'active',
      failed_logins_24h: d.hostname === 'proxmox-01' ? 14 : d.hostname === 'pi-dns' ? 3 : 0,
      logged_in_users: d.hostname === 'desktop-dev' ? 0 : 1,
      open_ports: d.hostname === 'proxmox-01' ? [22, 80, 443, 8006, 8080, 9090] : [22],
      apparmor: d.os.id === 'ubuntu' || d.os.id === 'debian' ? 'enabled' : undefined,
    },
    ups: makeUPS(d.hostname),
    updates: {
      package_manager: 'apt',
      total_installed: 340,
      pending_updates: d.hostname === 'pi-cameras' ? 12 : d.hostname === 'k3s-worker-01' ? 4 : d.hostname === 'backup-server' ? 7 : 0,
      pending_security_count: d.hostname === 'pi-cameras' ? 3 : d.hostname === 'backup-server' ? 2 : 0,
      pending_kernel_update: d.hostname === 'pi-cameras',
      pending_kernel_version: d.hostname === 'pi-cameras' ? '6.6.51-v8+' : undefined,
      unattended_upgrades: false,
      // Fields from the merged server UpdateInfo interface (unused by telemetry views)
      current_version: '', latest_version: '', update_available: false,
    },
  }
}

export function getDeviceDetail(id: string): DeviceDetailResponse | null {
  const devices = getDevices()
  const device = devices.find(d => d.id === id)
  if (!device) return null
  const def = deviceDefs.find(d => d.hostname === device.hostname)!
  return {
    device,
    latest_telemetry: {
      id: 1,
      device_id: id,
      timestamp: ago(30_000),
      data: makeTelemetry(def),
    },
    agent_connected: device.status !== 'offline',
  }
}

export function getDeviceHistory(id: string): TelemetrySnapshot[] {
  const devices = getDevices()
  const device = devices.find(d => d.id === id)
  if (!device) return []
  const def = deviceDefs.find(d => d.hostname === device.hostname)!
  return Array.from({ length: 50 }, (_, i) => ({
    id: 50 - i,
    device_id: id,
    timestamp: ago(i * 5 * MIN),
    data: makeTelemetry(def),
  }))
}

// ── Containers (on proxmox-01) ───────────────────────────────────────────────

const containerDefs = [
  { name: 'traefik', image: 'traefik:3.2', state: 'running', status: 'Up 14 days', group: 'Networking', cpu: 1.2, mem: 85, ports: '80,443', update: false },
  { name: 'plex', image: 'plexinc/pms-docker:latest', state: 'running', status: 'Up 14 days', group: 'Media', cpu: 8.5, mem: 1800, ports: '32400', update: true },
  { name: 'jellyfin', image: 'jellyfin/jellyfin:10.10.6', state: 'running', status: 'Up 14 days', group: 'Media', cpu: 3.2, mem: 640, ports: '8096', update: false },
  { name: 'homeassistant', image: 'ghcr.io/home-assistant/home-assistant:2025.3', state: 'running', status: 'Up 14 days', group: 'Automation', cpu: 4.1, mem: 420, ports: '8123', update: true },
  { name: 'grafana', image: 'grafana/grafana:11.5.2', state: 'running', status: 'Up 14 days', group: 'Monitoring', cpu: 0.8, mem: 180, ports: '3000', update: false },
  { name: 'prometheus', image: 'prom/prometheus:v3.2.1', state: 'running', status: 'Up 14 days', group: 'Monitoring', cpu: 2.5, mem: 350, ports: '9090', update: false },
  { name: 'portainer', image: 'portainer/portainer-ce:2.24.1', state: 'running', status: 'Up 14 days', group: 'Management', cpu: 0.3, mem: 95, ports: '9000', update: false },
  { name: 'nginx-proxy', image: 'nginx:1.27-alpine', state: 'running', status: 'Up 14 days', group: 'Networking', cpu: 0.1, mem: 32, ports: '8080', update: false },
  { name: 'uptime-kuma', image: 'louislam/uptime-kuma:1', state: 'running', status: 'Up 14 days', group: 'Monitoring', cpu: 0.6, mem: 110, ports: '3001', update: true },
  { name: 'vaultwarden', image: 'vaultwarden/server:1.33.2', state: 'running', status: 'Up 14 days', group: 'Security', cpu: 0.2, mem: 65, ports: '8880', update: false },
  { name: 'cloudflared', image: 'cloudflare/cloudflared:latest', state: 'running', status: 'Up 14 days', group: 'Networking', cpu: 0.4, mem: 48, ports: '', update: false },
  { name: 'watchtower', image: 'containrrr/watchtower:1.7.1', state: 'exited', status: 'Exited (0) 2 hours ago', group: 'Management', cpu: 0, mem: 0, ports: '', update: false },
]

export function getContainers(): ContainerInfo[] {
  return containerDefs.map((c, i) => ({
    id: `sha256:${uuid(c.name)}abcdef1234567890`,
    short_id: uuid(c.name).slice(0, 12),
    name: c.name,
    image: c.image,
    state: c.state,
    status: c.status,
    created: Math.floor((Date.now() - 14 * DAY) / 1000),
    ports: c.ports ? c.ports.split(',').map(p => ({ container_port: p, host_port: p, protocol: 'tcp' })) : [],
    labels: {},
    cpu_percent: c.cpu,
    mem_usage: c.mem * 1024 * 1024,
    mem_limit: 4 * 1024 * 1024 * 1024,
    restart_policy: 'unless-stopped',
    restart_count: 0,
    health_status: c.state === 'running' ? 'healthy' : undefined,
    riot: {
      group: c.group,
      name: c.name.charAt(0).toUpperCase() + c.name.slice(1),
      priority: i,
    },
    update_available: c.update || null,
  }))
}

// ── Events ───────────────────────────────────────────────────────────────────

const eventDefs: { device: string; type: string; severity: 'info' | 'warning' | 'critical'; message: string; ageMs: number; acked: boolean }[] = [
  { device: 'backup-server', type: 'ups_on_battery', severity: 'warning', message: 'UPS cyberpower switched to battery power', ageMs: 3 * MIN, acked: false },
  { device: 'pi-cameras', type: 'cpu_high', severity: 'critical', message: 'CPU usage above 85% for 10 minutes', ageMs: 8 * MIN, acked: false },
  { device: 'proxmox-01', type: 'container_update', severity: 'info', message: 'Container update available: plex (plexinc/pms-docker:latest)', ageMs: 25 * MIN, acked: false },
  { device: 'proxmox-01', type: 'failed_login', severity: 'warning', message: '14 failed SSH login attempts in the last 24h', ageMs: 1 * HOUR, acked: false },
  { device: 'pi-cameras', type: 'updates_available', severity: 'warning', message: '12 package updates available (3 security)', ageMs: 2 * HOUR, acked: false },
  { device: 'proxmox-01', type: 'container_update', severity: 'info', message: 'Container update available: homeassistant', ageMs: 3 * HOUR, acked: false },
  { device: 'desktop-dev', type: 'device_offline', severity: 'warning', message: 'Device went offline', ageMs: 3 * DAY, acked: true },
  { device: 'proxmox-01', type: 'container_restart', severity: 'info', message: 'Container restarted: watchtower (exit code 0)', ageMs: 2 * HOUR, acked: true },
  { device: 'backup-server', type: 'updates_available', severity: 'info', message: '7 package updates available (2 security)', ageMs: 4 * HOUR, acked: true },
  { device: 'nas-synology', type: 'disk_smart', severity: 'info', message: 'SMART check passed for all drives', ageMs: 6 * HOUR, acked: true },
  { device: 'k3s-worker-01', type: 'updates_available', severity: 'info', message: '4 package updates available', ageMs: 8 * HOUR, acked: true },
  { device: 'proxmox-01', type: 'container_started', severity: 'info', message: 'Container started: watchtower', ageMs: 10 * HOUR, acked: true },
  { device: 'pi-dns', type: 'service_restart', severity: 'info', message: 'Service restarted: pihole-FTL', ageMs: 12 * HOUR, acked: true },
  { device: 'proxmox-01', type: 'memory_high', severity: 'warning', message: 'Memory usage above 70% for 30 minutes', ageMs: 14 * HOUR, acked: true },
  { device: 'pi-cameras', type: 'temperature_high', severity: 'warning', message: 'CPU temperature above 75C', ageMs: 16 * HOUR, acked: true },
  { device: 'backup-server', type: 'backup_complete', severity: 'info', message: 'Restic backup completed successfully (3.2 GB)', ageMs: 1 * DAY, acked: true },
  { device: 'nas-synology', type: 'scrub_complete', severity: 'info', message: 'Btrfs scrub completed with 0 errors', ageMs: 1.5 * DAY, acked: true },
  { device: 'proxmox-01', type: 'agent_updated', severity: 'info', message: 'Agent updated from v0.9.1 to v0.9.2', ageMs: 2 * DAY, acked: true },
  { device: 'k3s-worker-02', type: 'agent_updated', severity: 'info', message: 'Agent updated from v0.9.1 to v0.9.2', ageMs: 2 * DAY, acked: true },
  { device: 'pi-dns', type: 'agent_updated', severity: 'info', message: 'Agent updated from v0.9.1 to v0.9.2', ageMs: 2 * DAY, acked: true },
  { device: 'nas-synology', type: 'agent_updated', severity: 'info', message: 'Agent updated from v0.9.1 to v0.9.2', ageMs: 2 * DAY, acked: true },
  { device: 'proxmox-01', type: 'cert_expiry', severity: 'info', message: 'TLS certificate expires in 60 days', ageMs: 3 * DAY, acked: true },
  { device: 'k3s-worker-01', type: 'device_online', severity: 'info', message: 'Device came online after reboot', ageMs: 3 * DAY, acked: true },
  { device: 'k3s-worker-02', type: 'device_online', severity: 'info', message: 'Device came online after reboot', ageMs: 3 * DAY, acked: true },
  { device: 'proxmox-01', type: 'container_update', severity: 'info', message: 'Container update available: uptime-kuma', ageMs: 4 * DAY, acked: true },
  { device: 'backup-server', type: 'backup_complete', severity: 'info', message: 'Restic backup completed successfully (2.8 GB)', ageMs: 4 * DAY, acked: true },
  { device: 'pi-cameras', type: 'cpu_high', severity: 'warning', message: 'CPU usage above 85% for 10 minutes', ageMs: 5 * DAY, acked: true },
  { device: 'proxmox-01', type: 'disk_usage', severity: 'info', message: 'Disk usage on / at 40%', ageMs: 6 * DAY, acked: true },
  { device: 'nas-synology', type: 'disk_usage', severity: 'info', message: 'Volume1 usage at 60%', ageMs: 7 * DAY, acked: true },
  { device: 'desktop-dev', type: 'device_online', severity: 'info', message: 'Device came online', ageMs: 10 * DAY, acked: true },
  { device: 'proxmox-01', type: 'container_started', severity: 'info', message: 'All containers started after system boot', ageMs: 14 * DAY, acked: true },
  { device: 'proxmox-01', type: 'device_online', severity: 'info', message: 'Device came online after reboot', ageMs: 14 * DAY, acked: true },
]

export function getEvents(): Event[] {
  const devices = getDevices()
  return eventDefs.map((e, i) => {
    const dev = devices.find(d => d.hostname === e.device)!
    return {
      id: eventDefs.length - i,
      device_id: dev.id,
      type: e.type,
      severity: e.severity,
      message: e.message,
      created_at: ago(e.ageMs),
      acknowledged_at: e.acked ? ago(e.ageMs - 5 * MIN) : undefined,
    }
  })
}

export function getUnreadCount(): number {
  return eventDefs.filter(e => !e.acked).length
}

// ── Fleet Summary ────────────────────────────────────────────────────────────

export function getSummary(): FleetSummary {
  const devices = getDevices()
  return {
    total_devices: devices.length,
    online_count: devices.filter(d => d.status === 'online').length,
    offline_count: devices.filter(d => d.status === 'offline').length,
    warning_count: devices.filter(d => d.status === 'warning').length,
    recent_events: eventDefs.filter(e => e.ageMs < DAY).length,
  }
}

// ── Probes ───────────────────────────────────────────────────────────────────

export function getProbes(): ProbeWithResult[] {
  const now = ago(0)
  return [
    {
      id: 1, name: 'Google DNS', type: 'ping', enabled: true,
      config: { target: '8.8.8.8' }, interval_seconds: 60, timeout_seconds: 5,
      created_at: ago(30 * DAY), updated_at: ago(30 * DAY),
      latest_result: { id: 1, probe_id: 1, success: true, latency_ms: 12, error_msg: '', metadata: {}, created_at: now },
    },
    {
      id: 2, name: 'Home Assistant', type: 'http', enabled: true,
      config: { url: 'http://10.0.10.5:8123', method: 'GET', expected_status: 200 }, interval_seconds: 30, timeout_seconds: 10,
      created_at: ago(30 * DAY), updated_at: ago(30 * DAY),
      latest_result: { id: 2, probe_id: 2, success: true, latency_ms: 45, status_code: 200, error_msg: '', metadata: {}, created_at: now },
    },
    {
      id: 3, name: 'Pi-hole DNS', type: 'dns', enabled: true,
      config: { target: '10.0.10.2', query: 'google.com', record_type: 'A' }, interval_seconds: 60, timeout_seconds: 5,
      created_at: ago(30 * DAY), updated_at: ago(30 * DAY),
      latest_result: { id: 3, probe_id: 3, success: true, latency_ms: 3, error_msg: '', metadata: { answer: '142.250.80.46' }, created_at: now },
    },
    {
      id: 4, name: 'External API', type: 'http', enabled: true,
      config: { url: 'https://api.example.com/health', method: 'GET', expected_status: 200 }, interval_seconds: 120, timeout_seconds: 15,
      created_at: ago(20 * DAY), updated_at: ago(20 * DAY),
      latest_result: { id: 4, probe_id: 4, success: false, latency_ms: 0, status_code: 0, error_msg: 'connection timed out', metadata: {}, created_at: now },
    },
  ]
}

export function getProbeResults(probeId: number): ProbeResult[] {
  const probe = getProbes().find(p => p.id === probeId)
  if (!probe) return []
  return Array.from({ length: 50 }, (_, i) => ({
    id: 50 - i,
    probe_id: probeId,
    success: probeId !== 4 || i > 5,
    latency_ms: probe.latest_result ? probe.latest_result.latency_ms + Math.floor(Math.random() * 10 - 5) : 0,
    status_code: probe.type === 'http' ? (probeId === 4 && i <= 5 ? 0 : 200) : undefined,
    error_msg: probeId === 4 && i <= 5 ? 'connection timed out' : '',
    metadata: {},
    created_at: ago(i * 2 * MIN),
  }))
}

// ── Alert Rules ──────────────────────────────────────────────────────────────

export function getAlertRules(): AlertRule[] {
  return [
    {
      id: 1, name: 'High CPU Usage', enabled: true,
      metric: 'cpu_usage', operator: '>', threshold: 85,
      target_name: '', target_state: '',
      severity: 'critical', device_filter: '*', cooldown_seconds: 600,
      notify: true, template_id: 'cpu_high',
      created_at: ago(60 * DAY), updated_at: ago(60 * DAY),
    },
    {
      id: 2, name: 'Device Offline', enabled: true,
      metric: 'device_status', operator: '==', threshold: 0,
      target_name: '', target_state: 'offline',
      severity: 'warning', device_filter: '*', cooldown_seconds: 300,
      notify: true, template_id: 'device_offline',
      created_at: ago(60 * DAY), updated_at: ago(60 * DAY),
    },
    {
      id: 3, name: 'Disk Usage Warning', enabled: true,
      metric: 'disk_usage', operator: '>', threshold: 80,
      target_name: '/', target_state: '',
      severity: 'warning', device_filter: '*', cooldown_seconds: 3600,
      notify: false, template_id: 'disk_high',
      created_at: ago(45 * DAY), updated_at: ago(45 * DAY),
    },
    {
      id: 4, name: 'UPS On Battery', enabled: true,
      metric: 'ups_on_battery', operator: '==', threshold: 1,
      target_name: '', target_state: '',
      severity: 'warning', device_filter: '*', cooldown_seconds: 900,
      notify: true, template_id: 'ups_on_battery',
      created_at: ago(30 * DAY), updated_at: ago(30 * DAY),
    },
    {
      id: 5, name: 'UPS Low Battery', enabled: true,
      metric: 'ups_battery_percent', operator: '<', threshold: 20,
      target_name: '', target_state: '',
      severity: 'critical', device_filter: '*', cooldown_seconds: 300,
      notify: true, template_id: 'ups_low_battery',
      created_at: ago(30 * DAY), updated_at: ago(30 * DAY),
    },
  ]
}

export function getAlertTemplates(): AlertTemplate[] {
  return [
    { id: 'cpu_high', name: 'High CPU', category: 'resource', metric: 'cpu_usage', operator: '>', threshold: 85, severity: 'critical', cooldown_seconds: 600, needs_target_name: false, description: 'Fires when CPU usage exceeds threshold' },
    { id: 'device_offline', name: 'Device Offline', category: 'availability', metric: 'device_status', operator: '==', threshold: 0, target_state: 'offline', severity: 'warning', cooldown_seconds: 300, needs_target_name: false, description: 'Fires when a device goes offline' },
    { id: 'disk_high', name: 'High Disk Usage', category: 'resource', metric: 'disk_usage', operator: '>', threshold: 80, severity: 'warning', cooldown_seconds: 3600, needs_target_name: true, description: 'Fires when disk usage exceeds threshold' },
    { id: 'memory_high', name: 'High Memory', category: 'resource', metric: 'memory_usage', operator: '>', threshold: 90, severity: 'critical', cooldown_seconds: 600, needs_target_name: false, description: 'Fires when memory usage exceeds threshold' },
    { id: 'container_stopped', name: 'Container Stopped', category: 'docker', metric: 'container_state', operator: '==', threshold: 0, target_state: 'exited', severity: 'warning', cooldown_seconds: 300, needs_target_name: true, description: 'Fires when a named container stops' },
    { id: 'ups_on_battery', name: 'UPS On Battery', category: 'ups', metric: 'ups_on_battery', operator: '==', threshold: 1, severity: 'warning', cooldown_seconds: 900, needs_target_name: false, description: 'Fires when the UPS switches to battery power' },
    { id: 'ups_low_battery', name: 'UPS Low Battery', category: 'ups', metric: 'ups_battery_percent', operator: '<', threshold: 20, severity: 'critical', cooldown_seconds: 300, needs_target_name: false, description: 'Fires when UPS battery charge drops below the threshold' },
  ]
}

// ── Notification Channels ────────────────────────────────────────────────────

export function getNotificationChannels(): NotificationChannel[] {
  return [
    {
      id: 1, name: 'ntfy Homelab', type: 'ntfy', enabled: true,
      config: { url: 'https://ntfy.sh', topic: 'homelab-alerts' },
      created_at: ago(60 * DAY), updated_at: ago(60 * DAY),
    },
  ]
}

export function getNotificationLog(): NotificationLog[] {
  return [
    { id: 1, channel_id: 1, event_id: 1, alert_rule_id: 1, status: 'sent', error_msg: '', created_at: ago(8 * MIN) },
    { id: 2, channel_id: 1, event_id: 6, alert_rule_id: 2, status: 'sent', error_msg: '', created_at: ago(3 * DAY) },
    { id: 3, channel_id: 1, event_id: 26, alert_rule_id: 1, status: 'sent', error_msg: '', created_at: ago(5 * DAY) },
  ]
}

// ── Security ─────────────────────────────────────────────────────────────────

export function getSecurityOverview() {
  return {
    total_devices: 8,
    devices_reporting: 7,
    total_failed_logins: 17,
    total_logged_in: 6,
    firewall_active: 7,
    firewall_inactive: 0,
    selinux_enforcing: 0,
    apparmor_enabled: 5,
  }
}

export function getSecurityDevices() {
  const devices = getDevices()
  return devices.filter(d => d.status !== 'offline').map(d => ({
    device_id: d.id,
    hostname: d.hostname,
    status: d.status,
    selinux: 'disabled',
    apparmor: d.tags.includes('dns') || d.tags.includes('k3s') || d.tags.includes('backup') || d.tags.includes('hypervisor') ? 'enabled' : 'disabled',
    firewall_status: 'active',
    failed_logins_24h: d.hostname === 'proxmox-01' ? 14 : d.hostname === 'pi-dns' ? 3 : 0,
    logged_in_users: 1,
    open_ports: d.hostname === 'proxmox-01' ? [22, 80, 443, 8006, 8080, 9090] : [22],
  }))
}

// ── Patch Status ─────────────────────────────────────────────────────────────

export function getPatchStatus(): DevicePatchInfo[] {
  const devices = getDevices()
  return [
    {
      device_id: devices.find(d => d.hostname === 'pi-cameras')!.id,
      hostname: 'pi-cameras', pending_updates: 12, security_count: 3, package_manager: 'apt',
    },
    {
      device_id: devices.find(d => d.hostname === 'backup-server')!.id,
      hostname: 'backup-server', pending_updates: 7, security_count: 2, package_manager: 'apt',
    },
    {
      device_id: devices.find(d => d.hostname === 'k3s-worker-01')!.id,
      hostname: 'k3s-worker-01', pending_updates: 4, security_count: 0, package_manager: 'apt',
    },
  ]
}

// ── Misc ─────────────────────────────────────────────────────────────────────

export function getAgentVersions() {
  return [
    { version: '0.9.2', count: 6 },
    { version: '0.9.1', count: 1 },
    { version: '0.9.0', count: 1 },
  ]
}

export function getCommands(): Command[] {
  return [
    {
      id: uuid('cmd-1'), device_id: getDevices()[0].id, action: 'container.restart', params: { container: 'watchtower' },
      status: 'completed', result_msg: 'Container restarted successfully', created_at: ago(2 * HOUR), updated_at: ago(2 * HOUR),
    },
  ]
}

export function getServerUpdate() {
  return {
    current_version: '0.9.2',
    latest_version: '0.9.2',
    update_available: false,
    server_host_device_id: getDevices()[0].id,
  }
}

export function getServerLogs() {
  return [
    { id: 1, timestamp: ago(1 * MIN), level: 'INFO', message: 'Telemetry received', attrs: { device: 'proxmox-01' }, source: 'handler' },
    { id: 2, timestamp: ago(2 * MIN), level: 'INFO', message: 'Heartbeat received', attrs: { device: 'pi-dns' }, source: 'handler' },
    { id: 3, timestamp: ago(5 * MIN), level: 'WARN', message: 'High CPU alert triggered', attrs: { device: 'pi-cameras', value: 87 }, source: 'alerter' },
    { id: 4, timestamp: ago(8 * MIN), level: 'INFO', message: 'Notification sent', attrs: { channel: 'ntfy Homelab', event_id: 1 }, source: 'notifier' },
    { id: 5, timestamp: ago(15 * MIN), level: 'INFO', message: 'Probe check completed', attrs: { probe: 'Home Assistant', latency_ms: 45 }, source: 'prober' },
    { id: 6, timestamp: ago(30 * MIN), level: 'INFO', message: 'Retention cleanup: removed 142 old heartbeats', source: 'retention' },
    { id: 7, timestamp: ago(1 * HOUR), level: 'WARN', message: 'Failed SSH login attempts detected', attrs: { device: 'proxmox-01', count: 14 }, source: 'alerter' },
    { id: 8, timestamp: ago(2 * HOUR), level: 'INFO', message: 'Command completed: container.restart', attrs: { device: 'proxmox-01', container: 'watchtower' }, source: 'commander' },
    { id: 9, timestamp: ago(4 * HOUR), level: 'INFO', message: 'Update check completed: server is up to date', source: 'updater' },
    { id: 10, timestamp: ago(6 * HOUR), level: 'INFO', message: 'Probe check failed', attrs: { probe: 'External API', error: 'connection timed out' }, source: 'prober' },
  ]
}

export function getBootstrapKeys() {
  return [
    { key_hash: 'demo-key-hash-1', label: 'Install script key', created_at: ago(60 * DAY), expires_at: ago(-30 * DAY), used: true, used_by_device: 'proxmox-01' },
  ]
}

export function getCerts() {
  return [
    { id: 1, device_id: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890', serial_number: 'AABBCCDD01001122', not_before: ago(90 * DAY), not_after: ago(-275 * DAY), revoked: false, created_at: ago(90 * DAY) },
    { id: 2, device_id: 'b2c3d4e5-f6a7-8901-bcde-f12345678901', serial_number: 'AABBCCDD02003344', not_before: ago(90 * DAY), not_after: ago(-275 * DAY), revoked: false, created_at: ago(90 * DAY) },
    { id: 3, device_id: 'c3d4e5f6-a7b8-9012-cdef-123456789012', serial_number: 'AABBCCDD03005566', not_before: ago(60 * DAY), not_after: ago(-305 * DAY), revoked: false, created_at: ago(60 * DAY) },
  ]
}

export function getServerCert() {
  return { fingerprint: 'SHA256:xK4vR9mT2bQ8nJ5pL1wF3hG7yD0cA6sE4uI2oN8kM3v' }
}

export function getHealth() {
  return { status: 'ok', database: true }
}

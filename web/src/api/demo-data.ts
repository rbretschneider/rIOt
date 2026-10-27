/**
 * Demo data factories for the static GitHub Pages demo.
 * All timestamps are computed relative to Date.now() so the data always looks fresh.
 */
import type {
  Device, DeviceDetailResponse, TelemetrySnapshot, FullTelemetryData,
  ContainerInfo, Event, FleetSummary, ProbeWithResult, ProbeResult,
  AlertRule, AlertTemplate, NotificationChannel, NotificationLog,
  Command, UPSInfo, WebServerInfo, USBInfo, SecurityScoreResult,
  AutomationConfig, ContainerLogEntry, DeviceProbeResult, DeviceProbeWithResultEnriched,
  FleetContainerRow, FleetHeartbeat, FleetHeartbeatsResponse, HeartbeatData,
  AutoUpdatePolicy, ActivityResponse, ExtraUpdater,
} from '../types/models'
import type { DevicePatchInfo, PatchBadge } from './client'
import { ApiError } from './errors'
import { demoAutoUpdateState } from './demoAutoUpdate'
import { REDACTED_SECRET } from '../utils/secrets'

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
    location: '',
    tags: d.tags,
    docker_available: d.hostname === 'proxmox-01',
    docker_container_count: d.hostname === 'proxmox-01' ? 12 : 0,
    docker_auto_update_container_count: 0,
    auto_patch: false,
    has_auto_update: false,
    hardware_profile: { ...d.hw, cpu_threads: d.hw.cpu_threads ?? d.hw.cpu_cores },
    last_heartbeat: d.status === 'offline' ? ago(3 * DAY) : ago((i + 1) * 15_000),
    last_telemetry: d.status === 'offline' ? ago(3 * DAY) : ago((i + 1) * 30_000),
    created_at: ago(90 * DAY + i * DAY),
    updated_at: d.status === 'offline' ? ago(3 * DAY) : ago((i + 1) * 30_000),
  }))
}

// ── Telemetry ────────────────────────────────────────────────────────────────

function makeUSB(hostname: string): USBInfo | undefined {
  if (hostname === 'proxmox-01') {
    return {
      devices: [
        { bus: '1', device: '2', vendor_id: '1a6e', product_id: '089a', vendor: 'Global Unichip Corp.', product: 'Coral USB Accelerator', serial: '', description: 'Global Unichip Corp. Coral USB Accelerator', device_class: 'Vendor Specific', speed_mbps: 5000, sys_path: '1-2' },
        { bus: '1', device: '3', vendor_id: '0658', product_id: '0200', vendor: 'Sigma Designs', product: 'Z-Wave Controller', serial: 'A1B2C3D4', description: 'Sigma Designs Z-Wave Controller', device_class: 'Communications', speed_mbps: 12, sys_path: '1-3' },
        { bus: '2', device: '1', vendor_id: '8087', product_id: '0026', vendor: 'Intel Corp.', product: 'AX201 Bluetooth', serial: '', description: 'Intel Corp. AX201 Bluetooth', device_class: 'Wireless', speed_mbps: 12, sys_path: '2-1' },
      ],
    }
  }
  if (hostname === 'nas-synology') {
    return {
      devices: [
        { bus: '1', device: '2', vendor_id: '174c', product_id: '55aa', vendor: 'ASMedia Technology', product: 'ASM1142 USB 3.1 Host Controller', serial: '', description: 'ASMedia Technology ASM1142 USB 3.1 Host Controller', device_class: 'Hub', speed_mbps: 5000, sys_path: '1-2' },
        { bus: '1', device: '4', vendor_id: '0764', product_id: '0501', vendor: 'Cyber Power System', product: 'CP1500 UPS', serial: 'ABCD1234', description: 'Cyber Power System CP1500 UPS', device_class: 'HID', speed_mbps: 1.5, sys_path: '1-4' },
      ],
    }
  }
  if (hostname === 'pi-dns') {
    return {
      devices: [
        { bus: '1', device: '2', vendor_id: '0424', product_id: 'ec00', vendor: 'Microchip Technology', product: 'SMSC9512/9514 Ethernet Adapter', serial: '', description: 'Microchip Technology SMSC9512/9514 Ethernet Adapter', device_class: 'Vendor Specific', speed_mbps: 480, sys_path: '1-1.1' },
      ],
    }
  }
  if (hostname === 'backup-server') {
    return {
      devices: [
        { bus: '1', device: '2', vendor_id: '0bc2', product_id: '2322', vendor: 'Seagate', product: 'Backup Plus Slim', serial: 'NA8DXYZ1', description: 'Seagate Backup Plus Slim', device_class: 'Mass Storage', speed_mbps: 5000, sys_path: '1-2' },
        { bus: '1', device: '3', vendor_id: '0bc2', product_id: '2322', vendor: 'Seagate', product: 'Backup Plus Slim', serial: 'NA8DXYZ2', description: 'Seagate Backup Plus Slim', device_class: 'Mass Storage', speed_mbps: 5000, sys_path: '1-3' },
        { bus: '2', device: '1', vendor_id: '0764', product_id: '0601', vendor: 'Cyber Power System', product: 'CP1500 UPS', serial: 'CPS-9876', description: 'Cyber Power System CP1500 UPS', device_class: 'HID', speed_mbps: 1.5, sys_path: '2-1' },
      ],
    }
  }
  if (hostname === 'pi-cameras') {
    return {
      devices: [
        { bus: '1', device: '2', vendor_id: '1415', product_id: '2000', vendor: 'Nam Tai E&E Products', product: 'Sony PS3 Eye Camera', serial: '', description: 'Nam Tai E&E Products Sony PS3 Eye Camera', device_class: 'Miscellaneous', speed_mbps: 480, sys_path: '1-1.2' },
        { bus: '1', device: '3', vendor_id: '0c45', product_id: '636d', vendor: 'Microdia', product: 'USB 2.0 Camera', serial: '', description: 'Microdia USB 2.0 Camera', device_class: 'Miscellaneous', speed_mbps: 480, sys_path: '1-1.3' },
      ],
    }
  }
  return undefined
}

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

function makeWebServers(hostname: string): WebServerInfo | undefined {
  if (hostname === 'proxmox-01') {
    const now = Date.now()
    return {
      servers: [{
        name: 'nginx',
        version: '1.24.0',
        status: 'running',
        pid: 1892,
        config_path: '/etc/nginx/nginx.conf',
        config_valid: true,
        sites: [
          {
            server_names: ['home.example.com'],
            listen: ['443 ssl', '80'],
            proxy_pass: 'http://127.0.0.1:8080',
            ssl_cert: '/etc/letsencrypt/live/home.example.com/fullchain.pem',
            enabled: true,
            config_file: '/etc/nginx/sites-enabled/home.conf',
          },
          {
            server_names: ['grafana.example.com'],
            listen: ['443 ssl'],
            proxy_pass: 'http://127.0.0.1:3000',
            ssl_cert: '/etc/letsencrypt/live/grafana.example.com/fullchain.pem',
            enabled: true,
            config_file: '/etc/nginx/sites-enabled/grafana.conf',
          },
          {
            server_names: ['media.example.com'],
            listen: ['443 ssl'],
            proxy_pass: 'http://10.0.10.50:8096',
            ssl_cert: '/etc/letsencrypt/live/media.example.com/fullchain.pem',
            enabled: true,
            config_file: '/etc/nginx/sites-enabled/media.conf',
          },
        ],
        certs: [
          {
            file_path: '/etc/letsencrypt/live/home.example.com/fullchain.pem',
            subject: 'home.example.com',
            issuer: "Let's Encrypt",
            sans: ['home.example.com', 'www.home.example.com'],
            not_before: new Date(now - 60 * DAY).toISOString(),
            not_after: new Date(now + 30 * DAY).toISOString(),
            days_left: 30,
            key_type: 'ECDSA',
            fingerprint: 'a1b2c3d4e5f6',
          },
          {
            file_path: '/etc/letsencrypt/live/grafana.example.com/fullchain.pem',
            subject: 'grafana.example.com',
            issuer: "Let's Encrypt",
            sans: ['grafana.example.com'],
            not_before: new Date(now - 80 * DAY).toISOString(),
            not_after: new Date(now + 10 * DAY).toISOString(),
            days_left: 10,
            key_type: 'ECDSA',
            fingerprint: 'b2c3d4e5f6g7',
          },
          {
            file_path: '/etc/letsencrypt/live/media.example.com/fullchain.pem',
            subject: 'media.example.com',
            issuer: "Let's Encrypt",
            sans: ['media.example.com'],
            not_before: new Date(now - 30 * DAY).toISOString(),
            not_after: new Date(now + 60 * DAY).toISOString(),
            days_left: 60,
            key_type: 'RSA',
            fingerprint: 'c3d4e5f6g7h8',
          },
        ],
        upstreams: [
          {
            name: 'backend_app',
            servers: [
              { address: '127.0.0.1:8080', weight: 1 },
              { address: '127.0.0.1:8081', weight: 1, backup: true },
            ],
          },
        ],
        security_config: {
          security_headers: {
            'Strict-Transport-Security': 'max-age=63072000; includeSubDomains; preload',
            'X-Frame-Options': 'DENY',
            'X-Content-Type-Options': 'nosniff',
            'Referrer-Policy': 'strict-origin-when-cross-origin',
          },
          rate_limiting: [
            { zone: 'api', rate: '10r/s', burst: 20 },
          ],
          access_controls: [
            { directive: 'allow', value: '10.0.10.0/24' },
            { directive: 'deny', value: 'all', location: '/admin' },
          ],
        },
      }],
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
          { mount_point: '/volume1', device: '/dev/md0', fs_type: 'btrfs', total_gb: 14400, used_gb: 8640, free_gb: 5760, usage_percent: 60, is_network_mount: false, is_pool: true },
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
    docker: (d.hostname === 'proxmox-01' || d.hostname === deviceDefs[1].hostname) ? {
      available: true,
      version: '27.4.1',
      api_version: '1.47',
      total_containers: d.hostname === 'proxmox-01' ? 12 : 2,
      running: d.hostname === 'proxmox-01' ? 11 : 2,
      stopped: d.hostname === 'proxmox-01' ? 1 : 0,
      paused: 0,
      images_total: 14,
      storage_driver: 'overlay2',
      containers: d.hostname === 'proxmox-01' ? getContainers() : getContainers().slice(0,2),
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
    web_servers: makeWebServers(d.hostname),
    usb: makeUSB(d.hostname),
    updates: {
      package_manager: 'apt',
      total_installed: 340,
      pending_updates: d.hostname === 'pi-cameras' ? 12 : d.hostname === 'k3s-worker-01' ? 4 : d.hostname === 'backup-server' ? 7 : 0,
      pending_security_count: d.hostname === 'pi-cameras' ? 3 : d.hostname === 'backup-server' ? 2 : 0,
      pending_kernel_update: d.hostname === 'pi-cameras',
      pending_kernel_version: d.hostname === 'pi-cameras' ? '6.6.51-v8+' : undefined,
      unattended_upgrades: false,
      // EXTRA-UPD-001: a DietPi host with an update waiting, and one whose DietPi status is unknown.
      extra_updaters: demoExtraUpdaters(d.hostname),
      // Fields from the merged server UpdateInfo interface (unused by telemetry views)
      current_version: '', latest_version: '', update_available: false,
    },
  }
}

// EXTRA-UPD-001 FR-060: demo hosts with a pending DietPi update and with an unknown one.
function demoExtraUpdaters(hostname: string): ExtraUpdater[] | undefined {
  if (hostname === 'pi-dns') {
    return [{
      id: 'dietpi', status: 'update_available', installed_version: '9.20.1', available_version: '10.8.2',
      pending_count: 1, readiness: 'enabled', notices: [{ code: 'major_upgrade' }],
    }]
  }
  if (hostname === 'pi-cameras') {
    return [{
      id: 'dietpi', status: 'unknown', unknown_reason: 'check_disabled', installed_version: '9.20.1',
      pending_count: 0, readiness: 'enabled',
    }]
  }
  return undefined
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
    labels: { 'com.docker.compose.project': c.group.toLowerCase(), 'com.docker.compose.service': c.name, 'com.docker.compose.project.working_dir': `/srv/stacks/${c.group.toLowerCase()}`, 'com.docker.compose.project.config_files': 'compose.yaml' },
    started_at: ago((i + 1) * HOUR),
    finished_at: c.state === 'exited' ? ago(2 * HOUR) : undefined,
    exit_code: c.state === 'exited' ? 0 : undefined,
    image_id: `sha256:demo-image-${i}`,
    mem_limit_configured: true,
    net_rx_rate: c.state === 'running' ? 10240 + i * 4096 : undefined,
    net_tx_rate: c.state === 'running' ? 5120 + i * 2048 : undefined,
    blk_read_rate: c.state === 'running' ? 2048 + i * 1024 : undefined,
    blk_write_rate: c.state === 'running' ? 1024 + i * 512 : undefined,
    cpu_percent: c.cpu,
    mem_usage: c.mem * 1024 * 1024,
    mem_limit: 4 * 1024 * 1024 * 1024,
    restart_policy: 'unless-stopped',
    restart_count: 0,
    health_status: c.state === 'running' ? i===8 ? 'unhealthy' : 'healthy' : undefined,
    riot: {
      group: c.group,
      name: c.name.charAt(0).toUpperCase() + c.name.slice(1),
      priority: i,
    },
    update_available: c.update || null,
    update_status: c.update ? 'update_available' : 'up_to_date',
  }))
}

// ── Events ───────────────────────────────────────────────────────────────────

const eventDefs: { device: string; type: string; severity: 'info' | 'warning' | 'critical'; message: string; ageMs: number; acked: boolean }[] = [
  { device: 'backup-server', type: 'ups_on_battery', severity: 'critical', message: 'UPS cyberpower switched to battery power', ageMs: 3 * MIN, acked: false },
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
      success_rate: 0.993, total_checks: 1440,
    },
    {
      id: 2, name: 'Home Assistant', type: 'http', enabled: true,
      config: { url: 'http://10.0.10.5:8123', method: 'GET', expected_status: 200 }, interval_seconds: 30, timeout_seconds: 10,
      created_at: ago(30 * DAY), updated_at: ago(30 * DAY),
      latest_result: { id: 2, probe_id: 2, success: true, latency_ms: 45, status_code: 200, error_msg: '', metadata: {}, created_at: now },
      success_rate: 1.0, total_checks: 2880,
    },
    {
      id: 3, name: 'Pi-hole DNS', type: 'dns', enabled: true,
      config: { target: '10.0.10.2', query: 'google.com', record_type: 'A' }, interval_seconds: 60, timeout_seconds: 5,
      created_at: ago(30 * DAY), updated_at: ago(30 * DAY),
      latest_result: { id: 3, probe_id: 3, success: true, latency_ms: 3, error_msg: '', metadata: { answer: '142.250.80.46' }, created_at: now },
      success_rate: 0.978, total_checks: 1440,
    },
    {
      id: 4, name: 'External API', type: 'http', enabled: true,
      config: { url: 'https://api.example.com/health', method: 'GET', expected_status: 200 }, interval_seconds: 120, timeout_seconds: 15,
      created_at: ago(20 * DAY), updated_at: ago(20 * DAY),
      latest_result: { id: 4, probe_id: 4, success: false, latency_ms: 0, status_code: 0, error_msg: 'connection timed out', metadata: {}, created_at: now },
      success_rate: 0.72, total_checks: 720,
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
      severity: 'critical', include_devices: '', exclude_devices: '', include_containers: '', exclude_containers: '', cooldown_seconds: 600,
      notify: true, template_id: 'cpu_high',
      created_at: ago(60 * DAY), updated_at: ago(60 * DAY),
    },
    {
      id: 2, name: 'Device Offline', enabled: true,
      metric: 'device_status', operator: '==', threshold: 0,
      target_name: '', target_state: 'offline',
      severity: 'warning', include_devices: '', exclude_devices: '', include_containers: '', exclude_containers: '', cooldown_seconds: 300,
      notify: true, template_id: 'device_offline',
      created_at: ago(60 * DAY), updated_at: ago(60 * DAY),
    },
    {
      id: 3, name: 'Disk Usage Warning', enabled: true,
      metric: 'disk_usage', operator: '>', threshold: 80,
      target_name: '/', target_state: '',
      severity: 'warning', include_devices: '', exclude_devices: '', include_containers: '', exclude_containers: '', cooldown_seconds: 3600,
      notify: false, template_id: 'disk_high',
      created_at: ago(45 * DAY), updated_at: ago(45 * DAY),
    },
    {
      id: 4, name: 'UPS On Battery', enabled: true,
      metric: 'ups_on_battery', operator: '==', threshold: 1,
      target_name: '', target_state: '',
      severity: 'warning', include_devices: '', exclude_devices: '', include_containers: '', exclude_containers: '', cooldown_seconds: 900,
      notify: true, template_id: 'ups_on_battery',
      created_at: ago(30 * DAY), updated_at: ago(30 * DAY),
    },
    {
      id: 5, name: 'UPS Low Battery', enabled: true,
      metric: 'ups_battery_percent', operator: '<', threshold: 20,
      target_name: '', target_state: '',
      severity: 'critical', include_devices: '', exclude_devices: '', include_containers: '', exclude_containers: '', cooldown_seconds: 300,
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
    { id: 'ups_on_battery', name: 'UPS On Battery', category: 'ups', metric: 'ups_on_battery', operator: '==', threshold: 1, severity: 'critical', cooldown_seconds: 900, needs_target_name: false, description: 'Fires when the UPS switches to battery power' },
    { id: 'ups_low_battery', name: 'UPS Low Battery', category: 'ups', metric: 'ups_battery_percent', operator: '<', threshold: 20, severity: 'critical', cooldown_seconds: 300, needs_target_name: false, description: 'Fires when UPS battery charge drops below the threshold' },
  ]
}

// ── Notification Channels ────────────────────────────────────────────────────

export function getNotificationChannels(): NotificationChannel[] {
  return [
    {
      id: 1, name: 'ntfy Homelab', type: 'ntfy', enabled: true,
      config: { server_url: 'https://ntfy.sh', topic: REDACTED_SECRET },
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
    certs_expiring_soon: 0,
    total_certs: getCerts().length,
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
    pending_security_count: d.hostname === 'proxmox-01' ? 2 : 0,
    unattended_upgrades: d.tags.includes('hypervisor') ? false : true,
    certs_expiring_soon: 0,
  }))
}

// ── Patch Status ─────────────────────────────────────────────────────────────

function demoBadge(kind: PatchBadge['kind'], label: string, reboot: string, needsYou: PatchBadge['explanation']['needs_you'] = [], info: PatchBadge['explanation']['needs_you'] = [], blocked: PatchBadge['explanation']['needs_you'] = []): PatchBadge {
  return {
    kind,
    label,
    explanation: {
      automatic: 'Yes. Any time (within the 360-minute cooldown).',
      reboot,
      needs_you: needsYou,
      blocked,
      info,
      needs_you_summary: needsYou.length === 0 ? 'Nothing.' : `${needsYou.length} item${needsYou.length === 1 ? '' : 's'} need${needsYou.length === 1 ? 's' : ''} you.`,
      as_of: '2026-10-19 02:00 UTC (just now)',
      notes: [],
    },
  }
}

export function getPatchStatus(): DevicePatchInfo[] {
  const devices = getDevices()
  return [
    {
      device_id: devices.find(d => d.hostname === 'pi-cameras')!.id,
      hostname: 'pi-cameras', pending_updates: 12, security_count: 3,
      reboot_class_count: 0, reboot_required: false, package_manager: 'apt',
      patch_badge: demoBadge('auto', 'Auto', 'Not needed.', [], [{
        code: 'extra_update_unknown', updater: 'dietpi', action: 'No action needed unless you expect an update.',
        text: 'DietPi update status is unknown: its own update check is turned off. rIOt will not apply it until an update is known to be waiting.',
      }]),
    },
    {
      device_id: devices.find(d => d.hostname === 'pi-dns')!.id,
      hostname: 'pi-dns', pending_updates: 0, security_count: 0,
      reboot_class_count: 0, reboot_required: false, package_manager: 'apt',
      extra_updaters: demoExtraUpdaters('pi-dns'),
      patch_badge: demoBadge('auto', 'Auto', 'Not needed.', [], [{
        code: 'extra_update_pending', updater: 'dietpi', action: 'Nothing to do.',
        text: 'DietPi 9.20.1 to 10.8.2 is waiting; it will be applied automatically at any time (within the cooldown).',
      }]),
    },
    {
      device_id: devices.find(d => d.hostname === 'nas-synology')!.id,
      hostname: 'nas-synology', pending_updates: 0, security_count: 0,
      reboot_class_count: 0, reboot_required: false, package_manager: 'apt',
      patch_badge: demoBadge('manual', 'Manual', 'Not needed.', [], [{
        code: 'auto_patch_off', text: 'Automatic patching is off for this host.', action: 'Turn on Auto-patch for this host.',
      }]),
    },
    {
      device_id: devices.find(d => d.hostname === 'backup-server')!.id,
      hostname: 'backup-server', pending_updates: 7, security_count: 2,
      reboot_class_count: 1, reboot_required: true, package_manager: 'apt',
      patch_badge: demoBadge('auto_reboot_unknown', 'Auto (reboot date Unknown)',
        'Automatic, date Unknown: the reboot happens on the next automatic patch run and the time is not predicted.'),
    },
    {
      // DIETPI-PATH-001: a refusal the operator cannot act on is Blocked, not "Needs you".
      device_id: devices.find(d => d.hostname === 'k3s-worker-02')!.id,
      hostname: 'k3s-worker-02', pending_updates: 0, security_count: 0,
      reboot_class_count: 0, reboot_required: false, package_manager: 'apt',
      patch_badge: demoBadge('blocked', 'Blocked', 'Not needed.', [], [], [{
        code: 'extra_blocked', updater: 'dietpi', action: 'Run riot-agent doctor on this host.',
        text: "DietPi update blocked: rIOt could not isolate /run/dietpi; this host's mount layout prevented rIOt from isolating the step",
      }]),
    },
    {
      device_id: devices.find(d => d.hostname === 'k3s-worker-01')!.id,
      hostname: 'k3s-worker-01', pending_updates: 4, security_count: 0,
      reboot_class_count: 2, reboot_required: false, package_manager: 'apt',
      patch_badge: demoBadge('needs_you', 'Needs you', 'Not needed.', [
        { code: 'auto_patch_off', text: 'Automatic patching is off for this host.', action: 'Turn on Auto-patch for this host.' },
      ]),
    },
  ]
}

// ── Misc ─────────────────────────────────────────────────────────────────────

// EXPECT-001: one in-flight container update so the demo shows an activity label.
export function getActivity(): ActivityResponse {
  const device = getDevices()[0]
  return {
    generated_at: new Date().toISOString(),
    newest_agent_version: '0.9.2',
    activities: [
      {
        device_id: device.id,
        kind: 'docker',
        phase: 'in_progress',
        action: 'docker_update',
        command_id: uuid('cmd-activity-1'),
        actor_phrase: 'by auto-update policy for nginx',
        started_at: ago(4 * MIN),
        back_by: null,
        waiting_dpkg_lock: false,
        progress: null,
        agent_from_version: '',
        agent_to_version: '',
        target: 'nginx',
        docker_actions: 1,
        covers_offline: false,
        device_online: true,
        escalation: null,
        powered_off_at: null,
      },
      // EXPECT-002: an agent self-update, shown as a spinner in the fleet Version column.
      {
        device_id: getDevices()[1].id,
        kind: 'agent',
        phase: 'in_progress',
        action: 'agent_update',
        command_id: uuid('cmd-activity-2'),
        actor_phrase: 'by agent auto-update',
        started_at: ago(1 * MIN),
        back_by: null,
        waiting_dpkg_lock: false,
        progress: null,
        agent_from_version: '0.9.1',
        agent_to_version: '0.9.2',
        target: '',
        docker_actions: 0,
        covers_offline: true,
        device_online: true,
        escalation: null,
        powered_off_at: null,
      },
    ],
  }
}

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
      status: 'succeeded', result_msg: 'Container restarted successfully', duration_ms: 1230, exit_code: 0, created_at: ago(2 * HOUR), updated_at: ago(2 * HOUR),
      origin: 'user', step: '', progress: null, failure_step: '', target_keys: [], target_key: '',
    },
    {
      id: uuid('cmd-2'), device_id: getDevices()[0].id, action: 'docker_update', params: { container: 'nginx' },
      status: 'failed', result_msg: 'rolled back to previous container', duration_ms: 48000, exit_code: null, created_at: ago(5 * HOUR), updated_at: ago(5 * HOUR),
      origin: 'auto_update', step: 'verify', progress: null, failure_step: 'verify', target_keys: ['docker:container:nginx'], target_key: 'docker:container:nginx',
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

export function getSecurityScore(deviceId: string): SecurityScoreResult {
  // Different scores per device for realistic demo
  const scores: Record<string, SecurityScoreResult> = {
    [uuid('proxmox-01')]: {
      overall_score: 72,
      max_score: 100,
      grade: 'C',
      evaluated_at: ago(MIN),
      categories: [
        {
          category: 'access_control', label: 'Access Control', score: 18, max_score: 22,
          findings: [
            { id: 'fw-active', category: 'access_control', severity: 'pass', title: 'Firewall enabled', description: 'Firewall is active', remediation: 'Enable UFW or iptables: sudo ufw enable', weight: 8, passed: true },
            { id: 'mac-enabled', category: 'access_control', severity: 'pass', title: 'Mandatory access control', description: 'AppArmor is enabled', remediation: 'Enable AppArmor or SELinux for process sandboxing', weight: 5, passed: true },
            { id: 'failed-logins-low', category: 'access_control', severity: 'pass', title: 'Failed login attempts', description: 'Low failed login count in last 24h', remediation: 'Investigate failed logins; consider fail2ban or SSH key-only auth', weight: 5, passed: true },
            { id: 'logged-in-users', category: 'access_control', severity: 'warning', title: 'Active user sessions', description: '4 active sessions — more than expected', remediation: "Review active sessions with 'who' and terminate unexpected ones", ref_url: 'https://www.cyberciti.biz/faq/unix-linux-who-command-examples-syntax-usage/', weight: 4, passed: false },
          ],
        },
        {
          category: 'patching', label: 'Patching', score: 15, max_score: 25,
          findings: [
            { id: 'no-security-updates', category: 'patching', severity: 'critical', title: 'Pending security updates', description: '3 pending security update(s)', remediation: 'Apply security updates: sudo apt upgrade or equivalent', ref_url: 'https://wiki.debian.org/SecurityManagement', weight: 10, passed: false },
            { id: 'pending-updates-low', category: 'patching', severity: 'warning', title: 'Pending package updates', description: '12 pending updates — system is behind', remediation: 'Run system updates regularly to stay current', ref_url: 'https://wiki.debian.org/AptCLI', weight: 5, passed: false },
            { id: 'no-kernel-update', category: 'patching', severity: 'pass', title: 'Kernel update pending', description: 'Kernel is up to date', remediation: 'Apply kernel update and reboot to activate', weight: 5, passed: true },
            { id: 'auto-updates', category: 'patching', severity: 'pass', title: 'Automatic security updates', description: 'Unattended upgrades enabled', remediation: 'Enable unattended-upgrades for automatic security patches', weight: 5, passed: true },
          ],
        },
        {
          category: 'network', label: 'Network', score: 17, max_score: 25,
          findings: [
            { id: 'minimal-open-ports', category: 'network', severity: 'warning', title: 'Open port count', description: '8 open ports — review for unnecessary services', remediation: 'Close unused ports and services to reduce attack surface', ref_url: 'https://www.cisecurity.org/benchmark/distribution_independent_linux', weight: 5, passed: false },
            { id: 'no-risky-ports', category: 'network', severity: 'pass', title: 'Insecure service ports', description: 'No insecure service ports open', remediation: 'Disable legacy protocols (FTP, Telnet) and use secure alternatives', weight: 5, passed: true },
            { id: 'tls-certs-valid', category: 'network', severity: 'critical', title: 'TLS certificate status', description: 'Certificate issues: media.home.lab (10d left)', remediation: 'Renew expiring certificates; consider automated renewal with certbot', ref_url: 'https://certbot.eff.org/', weight: 5, passed: false },
            { id: 'proxy-config-valid', category: 'network', severity: 'pass', title: 'Web server configuration', description: 'All web server configs are valid', remediation: 'Fix configuration errors shown in web server details', weight: 3, passed: true },
            { id: 'security-headers', category: 'network', severity: 'pass', title: 'Security headers configured', description: 'Security headers present (HSTS, X-Frame-Options, etc.)', remediation: 'Add HSTS, X-Frame-Options, X-Content-Type-Options, and CSP headers', weight: 4, passed: true },
            { id: 'rate-limiting', category: 'network', severity: 'pass', title: 'Rate limiting configured', description: 'Rate limiting is configured', remediation: 'Add rate limiting to protect against brute force and abuse', weight: 3, passed: true },
          ],
        },
        {
          category: 'docker', label: 'Docker', score: 7, max_score: 15,
          findings: [
            { id: 'container-restart-policy', category: 'docker', severity: 'pass', title: 'Container restart policies', description: 'All running containers have restart policies', remediation: "Set restart policy to 'unless-stopped' or 'on-failure' for all containers", weight: 4, passed: true },
            { id: 'container-health-checks', category: 'docker', severity: 'info', title: 'Container health checks', description: 'Some containers lack health checks', remediation: 'Add HEALTHCHECK to Dockerfiles or docker-compose health check config', ref_url: 'https://docs.docker.com/reference/dockerfile/#healthcheck', weight: 3, passed: false },
            { id: 'container-mem-limits', category: 'docker', severity: 'warning', title: 'Container memory limits', description: 'Some containers have no memory limit set', remediation: 'Set memory limits in docker-compose or with --memory flag to prevent OOM issues', ref_url: 'https://docs.docker.com/engine/containers/resource_constraints/', weight: 4, passed: false },
            { id: 'no-sensitive-mounts', category: 'docker', severity: 'warning', title: 'Sensitive volume mounts', description: 'Docker socket or other sensitive paths mounted read-write', remediation: 'Mount docker.sock read-only (:ro) or use a Docker socket proxy', ref_url: 'https://docs.docker.com/engine/security/#docker-daemon-attack-surface', weight: 4, passed: false },
          ],
        },
        {
          category: 'system', label: 'System', score: 10, max_score: 10,
          findings: [
            { id: 'no-failed-services', category: 'system', severity: 'pass', title: 'Failed systemd services', description: 'No failed services', remediation: "Investigate and fix failed services with 'systemctl status <service>'", weight: 5, passed: true },
            { id: 'reasonable-uptime', category: 'system', severity: 'pass', title: 'System uptime', description: 'Uptime: 14 days', remediation: 'Reboot to apply pending kernel updates', weight: 3, passed: true },
            { id: 'dns-configured', category: 'system', severity: 'pass', title: 'DNS configured', description: 'DNS servers: 10.0.10.2', remediation: 'Configure DNS servers in /etc/resolv.conf or systemd-resolved', weight: 2, passed: true },
          ],
        },
      ],
    },
    [uuid('pi-dns')]: {
      overall_score: 93,
      max_score: 100,
      grade: 'A',
      evaluated_at: ago(MIN),
      categories: [
        {
          category: 'access_control', label: 'Access Control', score: 22, max_score: 22,
          findings: [
            { id: 'fw-active', category: 'access_control', severity: 'pass', title: 'Firewall enabled', description: 'Firewall is active', remediation: 'Enable UFW or iptables', weight: 8, passed: true },
            { id: 'mac-enabled', category: 'access_control', severity: 'pass', title: 'Mandatory access control', description: 'AppArmor is enabled', remediation: 'Enable AppArmor or SELinux', weight: 5, passed: true },
            { id: 'failed-logins-low', category: 'access_control', severity: 'pass', title: 'Failed login attempts', description: 'Low failed login count in last 24h', remediation: 'Investigate failed logins', weight: 5, passed: true },
            { id: 'logged-in-users', category: 'access_control', severity: 'pass', title: 'Active user sessions', description: '1 active session(s)', remediation: 'Review active sessions', weight: 4, passed: true },
          ],
        },
        {
          category: 'patching', label: 'Patching', score: 25, max_score: 25,
          findings: [
            { id: 'no-security-updates', category: 'patching', severity: 'pass', title: 'Pending security updates', description: 'No pending security updates', remediation: 'Apply security updates', weight: 10, passed: true },
            { id: 'pending-updates-low', category: 'patching', severity: 'pass', title: 'Pending package updates', description: '2 pending update(s)', remediation: 'Run system updates regularly', weight: 5, passed: true },
            { id: 'no-kernel-update', category: 'patching', severity: 'pass', title: 'Kernel update pending', description: 'Kernel is up to date', remediation: 'Apply kernel update and reboot', weight: 5, passed: true },
            { id: 'auto-updates', category: 'patching', severity: 'pass', title: 'Automatic security updates', description: 'Unattended upgrades enabled', remediation: 'Enable unattended-upgrades', weight: 5, passed: true },
          ],
        },
        {
          category: 'system', label: 'System', score: 8, max_score: 10,
          findings: [
            { id: 'no-failed-services', category: 'system', severity: 'pass', title: 'Failed systemd services', description: 'No failed services', remediation: 'Investigate and fix failed services', weight: 5, passed: true },
            { id: 'reasonable-uptime', category: 'system', severity: 'info', title: 'System uptime', description: 'Uptime: 45 days', remediation: 'Reboot to apply pending kernel updates', weight: 3, passed: true },
            { id: 'dns-configured', category: 'system', severity: 'warning', title: 'DNS configured', description: 'No DNS servers configured', remediation: 'Configure DNS servers', ref_url: 'https://www.freedesktop.org/software/systemd/man/latest/systemd-resolved.service.html', weight: 2, passed: false },
          ],
        },
      ],
    },
  }

  // Default score for devices not explicitly defined
  const defaultScore: SecurityScoreResult = {
    overall_score: 68,
    max_score: 100,
    grade: 'C',
    evaluated_at: ago(MIN),
    categories: [
      {
        category: 'access_control', label: 'Access Control', score: 17, max_score: 22,
        findings: [
          { id: 'fw-active', category: 'access_control', severity: 'pass', title: 'Firewall enabled', description: 'Firewall is active', remediation: 'Enable UFW or iptables', weight: 8, passed: true },
          { id: 'mac-enabled', category: 'access_control', severity: 'warning', title: 'Mandatory access control', description: 'No mandatory access control detected', remediation: 'Enable AppArmor or SELinux', ref_url: 'https://www.redhat.com/en/topics/linux/what-is-selinux', weight: 5, passed: false },
          { id: 'failed-logins-low', category: 'access_control', severity: 'pass', title: 'Failed login attempts', description: 'Low failed login count in last 24h', remediation: 'Investigate failed logins', weight: 5, passed: true },
          { id: 'logged-in-users', category: 'access_control', severity: 'pass', title: 'Active user sessions', description: '1 active session(s)', remediation: 'Review active sessions', weight: 4, passed: true },
        ],
      },
      {
        category: 'patching', label: 'Patching', score: 15, max_score: 25,
        findings: [
          { id: 'no-security-updates', category: 'patching', severity: 'pass', title: 'Pending security updates', description: 'No pending security updates', remediation: 'Apply security updates', weight: 10, passed: true },
          { id: 'pending-updates-low', category: 'patching', severity: 'pass', title: 'Pending package updates', description: '3 pending update(s)', remediation: 'Run system updates regularly', weight: 5, passed: true },
          { id: 'no-kernel-update', category: 'patching', severity: 'warning', title: 'Kernel update pending', description: 'Kernel update available', remediation: 'Apply kernel update and reboot', ref_url: 'https://wiki.ubuntu.com/Kernel/LTSEnablementStack', weight: 5, passed: false },
          { id: 'auto-updates', category: 'patching', severity: 'info', title: 'Automatic security updates', description: 'Automatic updates not configured', remediation: 'Enable unattended-upgrades', ref_url: 'https://wiki.debian.org/UnattendedUpgrades', weight: 5, passed: false },
        ],
      },
      {
        category: 'system', label: 'System', score: 10, max_score: 10,
        findings: [
          { id: 'no-failed-services', category: 'system', severity: 'pass', title: 'Failed systemd services', description: 'No failed services', remediation: 'Investigate and fix failed services', weight: 5, passed: true },
          { id: 'reasonable-uptime', category: 'system', severity: 'pass', title: 'System uptime', description: 'Uptime: 7 days', remediation: 'Reboot to apply pending kernel updates', weight: 3, passed: true },
          { id: 'dns-configured', category: 'system', severity: 'pass', title: 'DNS configured', description: 'DNS servers: 10.0.10.2', remediation: 'Configure DNS servers', weight: 2, passed: true },
        ],
      },
    ],
  }

  return scores[deviceId] ?? defaultScore
}

// ── Fleet dashboard (heartbeats, containers) ─────────────────────────────────

/** Small deterministic hash so each device gets its own phase without randomness. */
function seedOf(text: string): number {
  return Math.abs(Array.from(text).reduce((a, c) => ((a << 5) - a + c.charCodeAt(0)) | 0, 7))
}

const clampPct = (v: number) => Math.max(1, Math.min(99, v))

/**
 * Heartbeat sample for one telemetry snapshot. `index` is the snapshot's position in
 * the series (0 = oldest) and varies the values deterministically, so charts show a
 * believable wave rather than a flat line. Also used by the single-device heartbeat history.
 */
export function toHeartbeatData(snapshot: TelemetrySnapshot, index = 0): HeartbeatData {
  const phase = seedOf(snapshot.device_id) % 17
  const wave = (period: number, amplitude: number) => Math.sin((index + phase) / period) * amplitude
  const cpu = snapshot.data.cpu?.usage_percent ?? 0
  const mem = snapshot.data.memory?.usage_percent ?? 0
  return {
    uptime: snapshot.data.os?.uptime ?? 0,
    cpu_percent: clampPct(cpu + wave(4, 12)),
    mem_percent: clampPct(mem + wave(9, 4)),
    load_avg_1m: Math.max(0.05, (snapshot.data.cpu?.load_avg_1m ?? 0) + wave(5, 0.4)),
    disk_root_percent: snapshot.data.disks?.filesystems?.[0]?.usage_percent ?? 0,
    disk_io_percent: clampPct(8 + wave(3, 6) + (phase % 5)),
    net_rx_bytes_sec: Math.round(120_000 + (wave(4, 1) + 1) * 90_000 + phase * 2_000),
    net_tx_bytes_sec: Math.round(40_000 + (wave(6, 1) + 1) * 30_000 + phase * 1_000),
  }
}

/** Heartbeat series for every demo device, oldest first (the order the real API returns). */
export function getFleetHeartbeats(): FleetHeartbeatsResponse {
  const devices: Record<string, FleetHeartbeat[]> = {}
  for (const device of getDevices()) {
    const history = getDeviceHistory(device.id).slice().reverse()
    devices[device.id] = history.map((snapshot, index) => ({
      device_id: device.id,
      timestamp: snapshot.timestamp,
      data: toHeartbeatData(snapshot, index),
    }))
  }
  return { since: ago(60 * MIN), until: ago(0), devices, devices_with_gpu: [] }
}

/** The Docker containers of the demo Docker host (proxmox-01), shaped for the fleet leaderboard. */
export function getFleetContainers(): FleetContainerRow[] {
  const host = getDevices()[0]
  return getContainers().map(c => ({
    device_id: host.id,
    hostname: host.hostname,
    container_id: c.id,
    container_name: c.name,
    image: c.image,
    stack: c.riot?.group ?? '',
    state: c.state,
    cpu_percent: c.cpu_percent,
    mem_usage: c.mem_usage,
    mem_limit: c.mem_limit,
    restart_count: c.restart_count ?? 0,
    update_available: c.update_available ?? null,
    update_status: c.update_status,
    auto_update_policy: demoAutoUpdateState(getAutoUpdatePolicies(host.id), c.name, c.labels),
  }))
}

// ── Auto-update policies ─────────────────────────────────────────────────────

// In-memory so the demo switches round-trip until the page reloads.
let autoUpdatePolicies: AutoUpdatePolicy[] | undefined

function policyStore(): AutoUpdatePolicy[] {
  if (!autoUpdatePolicies) {
    const host = getDevices()[0]
    autoUpdatePolicies = [
      { id: 1, device_id: host.id, target: 'plex', is_stack: false, compose_work_dir: '', enabled: true, created_at: ago(10 * DAY) },
      { id: 2, device_id: host.id, target: 'networking', is_stack: true, compose_work_dir: '/srv/stacks/networking', enabled: true, created_at: ago(5 * DAY) },
    ]
  }
  return autoUpdatePolicies
}

export function getAutoUpdatePolicies(deviceId: string): AutoUpdatePolicy[] {
  return policyStore().filter(p => p.device_id === deviceId).map(p => ({ ...p }))
}

/** Stores a policy with the server's rule: an enabled policy of the other kind under the same name is never replaced. */
export function setAutoUpdatePolicy(deviceId: string, target: string, isStack: boolean, workDir: string, enabled: boolean) {
  const store = policyStore()
  const existing = store.find(p => p.device_id === deviceId && p.target === target)
  if (existing && existing.is_stack !== isStack && existing.enabled) {
    const kind = existing.is_stack ? 'stack' : 'container'
    throw new ApiError(409, `an enabled ${kind} auto-update policy already uses this name`, 'auto_update_name_clash', { existing_kind: kind })
  }
  if (existing) Object.assign(existing, { is_stack: isStack, compose_work_dir: workDir, enabled })
  else store.push({ id: store.length + 1, device_id: deviceId, target, is_stack: isStack, compose_work_dir: workDir, enabled, created_at: new Date().toISOString() })
}

// ── Device probes ────────────────────────────────────────────────────────────

export function getDeviceProbes(): DeviceProbeWithResultEnriched[] {
  const host = getDevices()[0]
  const stamp = ago(0)
  return [
    {
      id: 101, device_id: host.id, device_hostname: host.hostname, name: 'Docker socket', type: 'shell', enabled: true,
      config: { command: 'docker info --format {{.ServerVersion}}', shell: '/bin/sh' },
      assertions: [{ field: 'exit_code', operator: '==', value: '0' }],
      interval_seconds: 60, timeout_seconds: 10, created_at: ago(30 * DAY), updated_at: ago(30 * DAY),
      latest_result: { id: 1001, probe_id: 101, device_id: host.id, success: true, latency_ms: 84, output: { exit_code: 0 }, created_at: stamp },
      success_rate: 1, total_checks: 1440,
    },
    {
      id: 102, device_id: host.id, device_hostname: host.hostname, name: 'Plex port', type: 'port', enabled: true,
      config: { host: '127.0.0.1', port: 32400 },
      assertions: [],
      interval_seconds: 30, timeout_seconds: 5, created_at: ago(20 * DAY), updated_at: ago(20 * DAY),
      latest_result: { id: 1002, probe_id: 102, device_id: host.id, success: true, latency_ms: 3, output: {}, created_at: stamp },
      success_rate: 0.998, total_checks: 2880,
    },
  ]
}

export function getDeviceProbeResults(probeId: number): DeviceProbeResult[] {
  const probe = getDeviceProbes().find(p => p.id === probeId)
  if (!probe) return []
  return Array.from({ length: 30 }, (_, i) => ({
    id: 30 - i,
    probe_id: probeId,
    device_id: probe.device_id,
    success: true,
    latency_ms: (probe.latest_result?.latency_ms ?? 5) + ((i * 7) % 9) - 4,
    output: {},
    created_at: ago(i * 2 * MIN),
  }))
}

/** Container log lines for the demo logs tab. */
export function getContainerLogs(containerId: string): ContainerLogEntry[] {
  const container = getContainers().find(c => c.id === containerId || c.short_id === containerId) ?? getContainers()[0]
  const lines: Array<[ContainerLogEntry['stream'], string]> = [
    ['stdout', 'service started'],
    ['stdout', 'listening on :8080'],
    ['stdout', 'GET /healthz 200 2ms'],
    ['stdout', 'GET /api/status 200 11ms'],
    ['stderr', 'warn: cache miss for key session:42'],
    ['stdout', 'GET /healthz 200 1ms'],
    ['stdout', 'POST /api/events 202 7ms'],
    ['stdout', 'GET /healthz 200 2ms'],
    ['stderr', 'warn: slow upstream response (412ms)'],
    ['stdout', 'GET /healthz 200 1ms'],
  ]
  return lines.map(([stream, line], i) => ({
    id: i + 1,
    container_id: container.id,
    container_name: container.name,
    timestamp: ago((lines.length - i) * 20_000),
    stream,
    line,
  }))
}

/** A short Markdown summary, the same shape as the server's device export. */
export function getDeviceSummaryMarkdown(id: string): string {
  const detail = getDeviceDetail(id)
  if (!detail) return ''
  const { device } = detail
  const hardware = device.hardware_profile
  return [
    `# ${device.hostname}`,
    '',
    `- Status: ${device.status}`,
    `- Architecture: ${device.arch}`,
    `- Agent: v${device.agent_version ?? 'unknown'}`,
    `- Address: ${device.primary_ip ?? 'unknown'}`,
    hardware ? `- CPU: ${hardware.cpu_model} (${hardware.cpu_cores} cores)` : '',
    hardware ? `- RAM: ${Math.round(hardware.total_ram_mb / 1024)} GB` : '',
    '',
    '_Demo data: nothing here is a real device._',
    '',
  ]
    .filter((line, i, all) => line !== '' || all[i - 1] !== '')
    .join('\n')
}

// ── Automation ───────────────────────────────────────────────────────────────

export function getAutomationConfig(): AutomationConfig {
  return {
    os_patch: {
      mode: 'window', start_time: '03:00', end_time: '05:00', cooldown_minutes: 60,
      stagger_seconds: 600, frequency: 'daily', reboot_class: 'off',
    },
    docker_update: {
      mode: 'anytime', start_time: '', end_time: '', cooldown_minutes: 60, stagger_seconds: 600,
    },
  }
}

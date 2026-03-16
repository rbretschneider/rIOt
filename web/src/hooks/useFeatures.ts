import { useQuery } from '@tanstack/react-query'
import { settingsApi } from '../api/settings'

// All toggleable features with their default state (true = visible).
export const FEATURES = {
  security_score: { label: 'Security Score', description: 'Security score column on fleet dashboard and score gauge on device detail' },
  docker: { label: 'Docker', description: 'Docker column on fleet dashboard and container sections on device detail' },
  usb: { label: 'USB Devices', description: 'USB device table on device detail' },
  hardware: { label: 'Hardware Details', description: 'PCI devices, disk drives, serial ports, GPUs on device detail' },
  web_servers: { label: 'Web Servers', description: 'Web server section on device detail' },
  ups: { label: 'UPS', description: 'UPS section on device detail' },
  security: { label: 'Security Details', description: 'Security details section on device detail (firewall, SELinux, etc.)' },
  network: { label: 'Network', description: 'Network interfaces section on device detail' },
  disk: { label: 'Filesystems', description: 'Filesystem / disk usage section on device detail' },
  services: { label: 'Services & Processes', description: 'Services and top processes sections on device detail' },
  updates: { label: 'Updates', description: 'Pending updates section on device detail' },
  logs: { label: 'Device Logs', description: 'Device log viewer on device detail' },
  cron: { label: 'Cron Jobs', description: 'Cron jobs (user crontabs, system crontabs) on device detail' },
  systemd_timers: { label: 'Systemd Timers', description: 'Systemd timer units on device detail' },
  device_terminal: { label: 'Device Terminal', description: 'SSH terminal access to devices from the dashboard' },
  docker_terminal: { label: 'Docker Terminal', description: 'Shell access to running Docker containers from the dashboard' },
} as const

export type FeatureKey = keyof typeof FEATURES

/**
 * Hook to get feature toggle state. Features default to enabled (true).
 * Returns { isEnabled, toggles, isLoading }.
 */
export function useFeatures() {
  const { data: toggles = {}, isLoading } = useQuery({
    queryKey: ['feature-toggles'],
    queryFn: settingsApi.getFeatureToggles,
    staleTime: 30_000,
  })

  const isEnabled = (key: FeatureKey): boolean => {
    // Default to true (visible) if not explicitly set
    return toggles[key] !== false
  }

  return { isEnabled, toggles, isLoading }
}

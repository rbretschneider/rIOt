import { useQuery } from '@tanstack/react-query'
import { settingsApi } from '../api/settings'

// All toggleable features with their default state (true = visible).
export const FEATURES = {
  security_score: { label: 'Security Score', description: 'Security score column on fleet dashboard and score gauge on device detail' },
  docker: { label: 'Docker', description: 'Docker column on fleet dashboard and container sections on device detail' },
  usb: { label: 'USB Devices', description: 'USB device table on device detail' },
  web_servers: { label: 'Web Servers', description: 'Web server section on device detail' },
  ups: { label: 'UPS', description: 'UPS section on device detail' },
  security: { label: 'Security Details', description: 'Security details section on device detail (firewall, SELinux, etc.)' },
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

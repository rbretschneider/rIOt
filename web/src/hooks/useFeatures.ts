import { useQuery } from '@tanstack/react-query'
import { settingsApi } from '../api/settings'

import { FEATURES, type FeatureKey } from './featureCatalog'

export { FEATURES, type FeatureKey }

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

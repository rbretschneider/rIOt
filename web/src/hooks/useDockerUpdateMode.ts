import { useQuery } from '@tanstack/react-query'
import { settingsApi } from '../api/settings'

/**
 * The Docker update window mode. Shares Settings > Agents' query, so saving the
 * window there also refreshes the Docker pages. Undefined while loading or on
 * error: callers then show no notice and keep the switches working.
 */
export function useDockerUpdateMode() {
  const query = useQuery({
    queryKey: ['automation-config'],
    queryFn: settingsApi.getAutomationConfig,
    refetchInterval: 15000,
  })
  return query.isError ? undefined : query.data?.docker_update?.mode
}

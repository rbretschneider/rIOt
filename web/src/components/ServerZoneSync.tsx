import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useTimePreference } from '../contexts/TimePreferenceProvider'

/**
 * Reads the server's display time zone once per page load and hands it to the
 * time preference, which validates it (acceptServerZone) before it can be used.
 * Renders nothing. Mounted only in the authenticated app: the endpoint is admin-only
 * and a 401 from the API client reloads the page. While the request is pending, or
 * after it failed (404, HTML from an older server, network error), the Server zone
 * formats as UTC (FR-050, NFR-006).
 */
export default function ServerZoneSync() {
  const { setServerZone } = useTimePreference()
  const { data } = useQuery({
    queryKey: ['time-display'],
    queryFn: api.getTimeDisplay,
    staleTime: Infinity,
    gcTime: Infinity,
    retry: 1,
  })

  const reported: unknown = data?.display_zone
  useEffect(() => {
    setServerZone(reported)
  }, [reported, setServerZone])

  return null
}

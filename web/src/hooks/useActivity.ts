import { useEffect, useMemo, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useWebSocket } from './useWebSocket'
import { indexActivities } from '../utils/activity'

/**
 * Server-computed expected activity per device (EXPECT-001). The first fetch
 * seeds the ['activity'] cache; `activity_update` WebSocket messages are applied
 * to it by useDevices. After a WebSocket reconnect the query is refetched, since
 * updates may have been missed while the socket was down. While the socket is
 * down the query also polls, as the device list does.
 */
export function useActivity() {
  const { connected } = useWebSocket(() => {})

  const query = useQuery({
    queryKey: ['activity'],
    queryFn: () => api.getActivity(),
    refetchInterval: connected ? false : 30_000,
  })

  // Initialized to true so the first connect does not trigger a second fetch.
  const prevConnected = useRef(true)
  const { refetch } = query
  useEffect(() => {
    if (connected && !prevConnected.current) void refetch()
    prevConnected.current = connected
  }, [connected, refetch])

  const byDevice = useMemo(() => indexActivities(query.data), [query.data])

  return {
    byDevice,
    newestAgentVersion: query.data?.newest_agent_version ?? '',
    isLoading: query.isLoading,
    isError: query.isError,
  }
}

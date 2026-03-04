import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback } from 'react'
import { api } from '../api/client'
import { useWebSocket } from './useWebSocket'
import type { Device, WSMessage } from '../types/models'

export function useDevices() {
  const queryClient = useQueryClient()

  const handleWS = useCallback((msg: WSMessage) => {
    if (msg.type === 'device_update') {
      queryClient.setQueryData<Device[]>(['devices'], (old) => {
        if (!old) return old
        const device = msg.data as Device
        const idx = old.findIndex((d) => d.id === device.id)
        if (idx >= 0) {
          const updated = [...old]
          updated[idx] = device
          return updated
        }
        return [...old, device]
      })
    } else if (msg.type === 'device_removed') {
      queryClient.setQueryData<Device[]>(['devices'], (old) =>
        old?.filter((d) => d.id !== msg.device_id)
      )
    } else if (msg.type === 'heartbeat') {
      // Refresh device list to get updated timestamps
      queryClient.invalidateQueries({ queryKey: ['devices'] })
    }
  }, [queryClient])

  const { connected } = useWebSocket(handleWS)

  const query = useQuery({
    queryKey: ['devices'],
    queryFn: api.getDevices,
    refetchInterval: 30_000,
  })

  return { ...query, wsConnected: connected }
}

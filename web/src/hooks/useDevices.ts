import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback } from 'react'
import { api } from '../api/client'
import { useWebSocket } from './useWebSocket'
import type { Device, HeartbeatData, WSMessage } from '../types/models'

export function useDevices() {
  const queryClient = useQueryClient()

  const handleWS = useCallback((msg: WSMessage) => {
    if (msg.type === 'device_update') {
      const device = msg.data as Device
      queryClient.setQueryData<Device[]>(['devices'], (old) => {
        if (!old) return [device]
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
    } else if (msg.type === 'heartbeat' && msg.device_id) {
      // Apply heartbeat data directly — update status + last_heartbeat in-place
      const hb = msg.data as HeartbeatData
      queryClient.setQueryData<Device[]>(['devices'], (old) => {
        if (!old) return old
        const idx = old.findIndex((d) => d.id === msg.device_id)
        if (idx < 0) return old
        const updated = [...old]
        updated[idx] = {
          ...updated[idx],
          status: 'online',
          last_heartbeat: new Date().toISOString(),
          ...(hb.agent_version ? { agent_version: hb.agent_version } : {}),
        }
        return updated
      })
      // Also push heartbeat into device detail cache if it's open
      queryClient.setQueryData(['device', msg.device_id], (old: any) => {
        if (!old) return old
        return {
          ...old,
          device: {
            ...old.device,
            status: 'online',
            last_heartbeat: new Date().toISOString(),
            ...(hb.agent_version ? { agent_version: hb.agent_version } : {}),
          },
          latest_heartbeat: hb,
        }
      })
    } else if (msg.type === 'telemetry' && msg.device_id) {
      // Push full telemetry directly into device detail cache
      queryClient.setQueryData(['device', msg.device_id], (old: any) => {
        if (!old) return old
        return {
          ...old,
          device: { ...old.device, last_telemetry: new Date().toISOString() },
          latest_telemetry: { device_id: msg.device_id, timestamp: new Date().toISOString(), data: msg.data },
        }
      })
    } else if (msg.type === 'docker_update' && msg.device_id) {
      // Docker container state changed — invalidate device detail to trigger refetch
      queryClient.invalidateQueries({ queryKey: ['device', msg.device_id] })
    } else if (msg.type === 'event') {
      // Prepend new event into events cache
      queryClient.setQueryData(['events'], (old: any) => {
        if (!old) return [msg.data]
        return [msg.data, ...old]
      })
      // Increment unread count for warning/critical events
      const evt = msg.data as { severity?: string }
      if (evt.severity === 'warning' || evt.severity === 'critical') {
        queryClient.setQueryData(['unread-count'], (old: { count: number } | undefined) => {
          return { count: (old?.count ?? 0) + 1 }
        })
      }
    }
  }, [queryClient])

  const { connected } = useWebSocket(handleWS)

  const query = useQuery({
    queryKey: ['devices'],
    queryFn: api.getDevices,
    refetchInterval: connected ? false : 30_000, // Only poll as fallback when WS is down
  })

  return { ...query, wsConnected: connected }
}

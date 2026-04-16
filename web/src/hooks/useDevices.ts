import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef } from 'react'
import { api } from '../api/client'
import { useWebSocket } from './useWebSocket'
import type { Device, HeartbeatData, FullTelemetryData, WSMessage, DockerEvent, ContainerInfo } from '../types/models'

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
      const telData = msg.data as FullTelemetryData
      const upsOnBattery = telData?.ups?.on_battery === true

      // If UPS is on battery, set fleet list status to warning (yellow dot)
      if (upsOnBattery) {
        queryClient.setQueryData<Device[]>(['devices'], (old) => {
          if (!old) return old
          const idx = old.findIndex((d) => d.id === msg.device_id)
          if (idx < 0) return old
          const updated = [...old]
          updated[idx] = { ...updated[idx], status: 'warning' }
          return updated
        })
      }

      // Push telemetry into device detail cache, merging with existing data.
      // The server strips heavy fields (processes, hardware, USB, etc.) from
      // WS broadcasts to save bandwidth, so we keep those from the prior fetch.
      queryClient.setQueryData(['device', msg.device_id], (old: any) => {
        if (!old) return old
        const existing = old.latest_telemetry?.data as FullTelemetryData | undefined
        const merged: Record<string, unknown> = { ...existing }
        for (const [key, value] of Object.entries(telData)) {
          if (value != null) {
            merged[key] = value
          }
        }
        return {
          ...old,
          device: {
            ...old.device,
            ...(upsOnBattery ? { status: 'warning' as const } : {}),
            last_telemetry: new Date().toISOString(),
          },
          latest_telemetry: { device_id: msg.device_id, timestamp: new Date().toISOString(), data: merged },
        }
      })
    } else if (msg.type === 'docker_update' && msg.device_id) {
      const evt = msg.data as DockerEvent
      // Significant actions get a targeted state update in the device detail
      // cache. We never call invalidateQueries on ['device', ...] here — that
      // races with the telemetry WS setQueryData merge and causes "container
      // not found" flicker. The telemetry push is the authoritative full refresh.
      const significant = ['start', 'stop', 'die', 'create', 'destroy', 'pause', 'unpause']
      if (evt?.action && significant.includes(evt.action)) {
        const stateMap: Record<string, string> = {
          start: 'running',
          stop: 'exited',
          die: 'exited',
          pause: 'paused',
          unpause: 'running',
          // 'create' and 'destroy' intentionally omitted — no state to apply
        }
        const newState = stateMap[evt.action]
        if (newState) {
          // Strip leading '/' that Docker sometimes includes in container_name
          const name = evt.container_name?.replace(/^\//, '')
          queryClient.setQueryData(['device', msg.device_id], (old: any) => {
            if (!old?.latest_telemetry?.data?.docker?.containers) return old
            const containers = old.latest_telemetry.data.docker.containers as ContainerInfo[]
            const idx = containers.findIndex((c: ContainerInfo) => c.name === name)
            if (idx < 0) return old
            const updatedContainers = [...containers]
            updatedContainers[idx] = { ...updatedContainers[idx], state: newState }
            return {
              ...old,
              latest_telemetry: {
                ...old.latest_telemetry,
                data: {
                  ...old.latest_telemetry.data,
                  docker: {
                    ...old.latest_telemetry.data.docker,
                    containers: updatedContainers,
                  },
                },
              },
            }
          })
        }
      }
      // Fleet list invalidation remains for all docker_update events (unchanged)
      queryClient.invalidateQueries({ queryKey: ['devices'] })
    }
    // Note: 'event' messages are handled by GlobalWSHandler in App.tsx
    // so the alert badge updates on ALL pages, not just pages using useDevices().
  }, [queryClient])

  // Track the previous connected state so we can detect the false→true
  // transition (reconnect) without firing on the initial connection.
  // Initialized to true so the first connect does not trigger an invalidation.
  const prevConnectedRef = useRef(true)

  const { connected } = useWebSocket(handleWS)

  // On WS reconnect, invalidate all cached device detail queries so they
  // refetch fresh data. This covers the window where the WS was down and
  // we may have missed telemetry pushes.
  useEffect(() => {
    if (connected && !prevConnectedRef.current) {
      queryClient.invalidateQueries({ queryKey: ['device'], exact: false })
    }
    prevConnectedRef.current = connected
  }, [connected, queryClient])

  const query = useQuery({
    queryKey: ['devices'],
    queryFn: api.getDevices,
    refetchInterval: connected ? false : 30_000, // Only poll as fallback when WS is down
  })

  return { ...query, wsConnected: connected }
}

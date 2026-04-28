import { useCallback, useRef, useState } from 'react'
import { useWebSocket } from './useWebSocket'
import type { WSMessage, HeartbeatData } from '../types/models'

/**
 * Subscribes to WebSocket `heartbeat` messages and exposes a map of
 * `{ [deviceId]: lastPulseAt }` (timestamp number in ms).
 *
 * DeviceCard uses this to re-key its status-dot CSS animation on each new
 * heartbeat, producing the pulse effect (FR-032 / AC-014).
 */
export function usePerDevicePulse(): Record<string, number> {
  const [pulseMap, setPulseMap] = useState<Record<string, number>>({})
  const pulseRef = useRef<Record<string, number>>({})

  const handleWS = useCallback((msg: WSMessage) => {
    if (msg.type === 'heartbeat' && msg.device_id) {
      // Update ref synchronously (no re-render) then flush to state so the
      // card's animation key changes. Using functional update avoids stale
      // closures in the handler.
      const deviceId = msg.device_id
      const now = Date.now()
      pulseRef.current = { ...pulseRef.current, [deviceId]: now }
      setPulseMap(prev => ({ ...prev, [deviceId]: now }))
    }
  }, [])

  useWebSocket(handleWS)

  return pulseMap
}

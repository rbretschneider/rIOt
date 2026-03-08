import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import type { WSMessage } from '../types/models'

interface WSContextValue {
  connected: boolean
  subscribe: (handler: (msg: WSMessage) => void) => () => void
}

const WSContext = createContext<WSContextValue>({
  connected: false,
  subscribe: () => () => {},
})

export function WebSocketProvider({ children }: { children: React.ReactNode }) {
  const [connected, setConnected] = useState(false)
  const handlersRef = useRef(new Set<(msg: WSMessage) => void>())
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeout = useRef<ReturnType<typeof setTimeout>>(undefined)

  const connect = useCallback(() => {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${proto}//${window.location.host}/ws`)

    ws.onopen = () => setConnected(true)

    ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data)
        handlersRef.current.forEach(handler => handler(msg))
      } catch {
        // ignore parse errors
      }
    }

    ws.onclose = (event) => {
      setConnected(false)
      wsRef.current = null
      // 1008 = policy violation (auth failure) — don't reconnect
      if (event.code === 1008) {
        window.location.reload()
        return
      }
      reconnectTimeout.current = setTimeout(connect, 3000)
    }

    ws.onerror = () => ws.close()

    wsRef.current = ws
  }, [])

  useEffect(() => {
    connect()
    return () => {
      clearTimeout(reconnectTimeout.current)
      wsRef.current?.close()
    }
  }, [connect])

  const subscribe = useCallback((handler: (msg: WSMessage) => void) => {
    handlersRef.current.add(handler)
    return () => { handlersRef.current.delete(handler) }
  }, [])

  return (
    <WSContext.Provider value={{ connected, subscribe }}>
      {children}
    </WSContext.Provider>
  )
}

/** Subscribe to the shared WebSocket connection. */
export function useWebSocket(onMessage: (msg: WSMessage) => void) {
  const { connected, subscribe } = useContext(WSContext)
  const onMessageRef = useRef(onMessage)
  onMessageRef.current = onMessage

  useEffect(() => {
    return subscribe((msg) => onMessageRef.current(msg))
  }, [subscribe])

  return { connected }
}

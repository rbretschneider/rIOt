/**
 * Demo replacement for contexts/WebSocketProvider — no-op WebSocket, renders children.
 * Vite aliases this module over WebSocketProvider.tsx when VITE_DEMO=true.
 */
import type { WSMessage } from '../types/models'

export function WebSocketProvider({ children }: { children: React.ReactNode }) {
  return <>{children}</>
}

export function useWebSocket(_onMessage: (msg: WSMessage) => void) {
  return { connected: false }
}

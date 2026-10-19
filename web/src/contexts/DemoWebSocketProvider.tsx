/**
 * Demo replacement for contexts/WebSocketProvider — no-op WebSocket, renders children.
 * Vite aliases this module over WebSocketProvider.tsx when VITE_DEMO=true.
 */
import type { WSMessage } from '../types/models'

export function WebSocketProvider({ children }: { children: React.ReactNode }) {
  return <>{children}</>
}

// The demo has no socket to lose, so it reports a healthy connection: the dashboard must
// not show its "Live updates paused" notice.
export function useWebSocket(_onMessage: (msg: WSMessage) => void) {
  return { connected: true }
}

import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { WebSocketProvider, useWebSocket } from './DemoWebSocketProvider'

function Probe() {
  const { connected } = useWebSocket(() => {})
  return <span data-testid="connected">{String(connected)}</span>
}

describe('[UIF AC-126] demo WebSocket reports connected', () => {
  it('useWebSocket() returns connected: true', () => {
    render(
      <WebSocketProvider>
        <Probe />
      </WebSocketProvider>,
    )
    expect(screen.getByTestId('connected').textContent).toBe('true')
  })

  it('renders its children untouched', () => {
    render(
      <WebSocketProvider>
        <p>child content</p>
      </WebSocketProvider>,
    )
    expect(screen.getByText('child content')).toBeInTheDocument()
  })
})

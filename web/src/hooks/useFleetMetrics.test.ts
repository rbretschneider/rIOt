import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import type { WSMessage, HeartbeatData } from '../types/models'

// ---------------------------------------------------------------------------
// Mock setup
//
// useDashboardTick is mocked to a ref-stable counter so the tick-driven
// useMemo inside useFleetMetrics does not produce infinite re-renders in test.
// We increment it manually when we want to simulate a tick.
// ---------------------------------------------------------------------------

vi.mock('./useWebSocket', () => ({
  useWebSocket: (_handler: (msg: WSMessage) => void) => {
    return { connected: true }
  },
}))

// Return a stable 0 — the tick-path useMemo won't fire state updates when
// the buffer is empty, so this is safe.
vi.mock('./useDashboardTick', () => ({
  useDashboardTick: () => 0,
}))

const mockGetFleetHeartbeats = vi.fn()

vi.mock('../api/client', () => ({
  api: {
    getFleetHeartbeats: (...args: unknown[]) => mockGetFleetHeartbeats(...args),
  },
}))

afterEach(() => {
  vi.clearAllMocks()
})

function makeWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children)
}

// ---------------------------------------------------------------------------
// [AC-014] netRxPoints / netTxPoints populated from heartbeat data
// ---------------------------------------------------------------------------

describe('[AC-014] netRxPoints/netTxPoints populated from heartbeat data', () => {
  it('netRxPoints[0].value equals net_rx_bytes_sec from the REST seed', async () => {
    mockGetFleetHeartbeats.mockResolvedValue({
      since: new Date(0).toISOString(),
      until: new Date().toISOString(),
      devices: {
        'dev-1': [
          {
            device_id: 'dev-1',
            timestamp: '2024-01-01T00:00:00Z',
            data: {
              uptime: 3600,
              cpu_percent: 45,
              mem_percent: 60,
              load_avg_1m: 1.2,
              disk_root_percent: 70,
              net_rx_bytes_sec: 1000,
              net_tx_bytes_sec: 500,
            } as HeartbeatData,
          },
        ],
      },
      devices_with_gpu: [],
    })

    const { useFleetMetrics } = await import('./useFleetMetrics')
    const { result } = renderHook(
      () => useFleetMetrics([], []),
      { wrapper: makeWrapper() },
    )

    await waitFor(() => {
      expect(result.current.perDeviceSeries.length).toBeGreaterThan(0)
    })

    const series = result.current.perDeviceSeries.find(s => s.deviceId === 'dev-1')
    expect(series).toBeDefined()
    expect(series!.netRxPoints).toHaveLength(1)
    expect(series!.netRxPoints[0].value).toBe(1000)
  })

  it('netTxPoints[0].value equals net_tx_bytes_sec from the REST seed', async () => {
    mockGetFleetHeartbeats.mockResolvedValue({
      since: new Date(0).toISOString(),
      until: new Date().toISOString(),
      devices: {
        'dev-1': [
          {
            device_id: 'dev-1',
            timestamp: '2024-01-01T00:00:00Z',
            data: {
              uptime: 3600,
              cpu_percent: 45,
              mem_percent: 60,
              load_avg_1m: 1.2,
              disk_root_percent: 70,
              net_rx_bytes_sec: 1000,
              net_tx_bytes_sec: 500,
            } as HeartbeatData,
          },
        ],
      },
      devices_with_gpu: [],
    })

    const { useFleetMetrics } = await import('./useFleetMetrics')
    const { result } = renderHook(
      () => useFleetMetrics([], []),
      { wrapper: makeWrapper() },
    )

    await waitFor(() => {
      expect(result.current.perDeviceSeries.length).toBeGreaterThan(0)
    })

    const series = result.current.perDeviceSeries.find(s => s.deviceId === 'dev-1')
    expect(series).toBeDefined()
    expect(series!.netTxPoints[0].value).toBe(500)
  })

  it('[AC-016] missing net fields default to 0 (old-agent compatibility)', async () => {
    mockGetFleetHeartbeats.mockResolvedValue({
      since: new Date(0).toISOString(),
      until: new Date().toISOString(),
      devices: {
        'dev-old': [
          {
            device_id: 'dev-old',
            timestamp: '2024-01-01T00:00:00Z',
            data: {
              uptime: 100,
              cpu_percent: 10,
              mem_percent: 20,
              load_avg_1m: 0.5,
              disk_root_percent: 30,
              // no net_rx_bytes_sec / net_tx_bytes_sec — old agent
            } as HeartbeatData,
          },
        ],
      },
      devices_with_gpu: [],
    })

    const { useFleetMetrics } = await import('./useFleetMetrics')
    const { result } = renderHook(
      () => useFleetMetrics([], []),
      { wrapper: makeWrapper() },
    )

    await waitFor(() => {
      expect(result.current.perDeviceSeries.length).toBeGreaterThan(0)
    })

    const series = result.current.perDeviceSeries.find(s => s.deviceId === 'dev-old')
    expect(series).toBeDefined()
    expect(series!.netRxPoints[0].value).toBe(0)
    expect(series!.netTxPoints[0].value).toBe(0)
  })

  it('[SEC-FLEET-NET-001] Infinity and negative values are clamped to 0 by finiteOrZero', async () => {
    mockGetFleetHeartbeats.mockResolvedValue({
      since: new Date(0).toISOString(),
      until: new Date().toISOString(),
      devices: {
        'dev-bad': [
          {
            device_id: 'dev-bad',
            timestamp: '2024-01-01T00:00:00Z',
            data: {
              uptime: 100,
              cpu_percent: 10,
              mem_percent: 20,
              load_avg_1m: 0.5,
              disk_root_percent: 30,
              net_rx_bytes_sec: Infinity,  // non-finite — must be clamped to 0
              net_tx_bytes_sec: -1,         // negative — must be clamped to 0
            } as HeartbeatData,
          },
        ],
      },
      devices_with_gpu: [],
    })

    const { useFleetMetrics } = await import('./useFleetMetrics')
    const { result } = renderHook(
      () => useFleetMetrics([], []),
      { wrapper: makeWrapper() },
    )

    await waitFor(() => {
      expect(result.current.perDeviceSeries.length).toBeGreaterThan(0)
    })

    const series = result.current.perDeviceSeries.find(s => s.deviceId === 'dev-bad')
    expect(series).toBeDefined()
    expect(series!.netRxPoints[0].value).toBe(0)
    expect(series!.netTxPoints[0].value).toBe(0)
  })
})

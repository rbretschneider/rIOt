import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import type { WSMessage, ContainerInfo } from '../types/models'

// ---------------------------------------------------------------------------
// Mock setup
//
// We control two things:
//   1. capturedHandler — the WS message handler registered by useWebSocket,
//      so we can push messages directly in tests.
//   2. mockConnected — the connected state returned by useWebSocket, so we
//      can simulate reconnection transitions.
//
// IMPORTANT: The mock must be declared at the module level (not inside tests)
// so Vitest can hoist it. We use a mutable object to work around the ESM
// limitation that vi.mock factory closures can't close over variables defined
// after the mock declaration.
// ---------------------------------------------------------------------------

const wsState = {
  connected: false,
  handler: null as ((msg: WSMessage) => void) | null,
}

vi.mock('./useWebSocket', () => ({
  useWebSocket: (handler: (msg: WSMessage) => void) => {
    wsState.handler = handler
    return { connected: wsState.connected }
  },
}))

vi.mock('../api/client', () => ({
  api: {
    getDevices: vi.fn().mockResolvedValue([]),
  },
}))

// Import the module under test AFTER the mocks are declared.
import { useDevices } from './useDevices'

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

function makeQC() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function makeWrapper(qc: QueryClient) {
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children)
}

// Minimal cached device detail response
const baseContainer: ContainerInfo = {
  id: 'abc123full',
  short_id: 'abc123',
  name: 'my-app',
  image: 'nginx:latest',
  state: 'running',
  status: 'Up 2 hours',
  created: 0,
  cpu_percent: 0.5,
  mem_usage: 50_000_000,
  mem_limit: 200_000_000,
}

const baseDeviceDetail = {
  device: { id: 'dev-1', hostname: 'host-1', status: 'online' as const },
  latest_telemetry: {
    device_id: 'dev-1',
    timestamp: '2026-01-01T00:00:00Z',
    data: {
      docker: {
        available: true,
        containers: [baseContainer],
      },
    },
  },
}

beforeEach(() => {
  wsState.connected = false
  wsState.handler = null
})

// ---------------------------------------------------------------------------
// AC-001, AC-008: docker_update with significant action uses setQueryData,
//                  NOT invalidateQueries on ['device', deviceId]
// ---------------------------------------------------------------------------

describe('[AC-001] [AC-008] docker_update significant action updates container state in cache without invalidating device detail', () => {
  it('sets container state to "exited" in cache when action is "stop"', () => {
    const qc = makeQC()
    qc.setQueryData(['device', 'dev-1'], baseDeviceDetail)

    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries')

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    act(() => {
      wsState.handler!({
        type: 'docker_update',
        device_id: 'dev-1',
        data: { container_id: 'abc123full', container_name: 'my-app', action: 'stop' },
      })
    })

    const updated = qc.getQueryData<typeof baseDeviceDetail>(['device', 'dev-1'])
    const containers = updated!.latest_telemetry.data.docker.containers
    expect(containers[0].state, 'container state should be "exited" after stop').toBe('exited')

    // invalidateQueries must NOT have been called with the device detail key
    const deviceDetailCalls = invalidateSpy.mock.calls.filter((args) => {
      const opts = args[0] as { queryKey?: unknown[] } | undefined
      return Array.isArray(opts?.queryKey) &&
        opts.queryKey[0] === 'device' &&
        opts.queryKey[1] === 'dev-1'
    })
    expect(deviceDetailCalls.length, 'invalidateQueries must not be called on [device, dev-1]').toBe(0)
  })

  it('sets container state to "running" in cache when action is "start"', () => {
    const qc = makeQC()
    const stoppedDetail = {
      ...baseDeviceDetail,
      latest_telemetry: {
        ...baseDeviceDetail.latest_telemetry,
        data: {
          docker: { available: true, containers: [{ ...baseContainer, state: 'exited' }] },
        },
      },
    }
    qc.setQueryData(['device', 'dev-1'], stoppedDetail)

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    act(() => {
      wsState.handler!({
        type: 'docker_update',
        device_id: 'dev-1',
        data: { container_id: 'abc123full', container_name: 'my-app', action: 'start' },
      })
    })

    const updated = qc.getQueryData<typeof baseDeviceDetail>(['device', 'dev-1'])
    expect(updated!.latest_telemetry.data.docker.containers[0].state).toBe('running')
  })

  it('sets container state to "paused" in cache when action is "pause"', () => {
    const qc = makeQC()
    qc.setQueryData(['device', 'dev-1'], baseDeviceDetail)

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    act(() => {
      wsState.handler!({
        type: 'docker_update',
        device_id: 'dev-1',
        data: { container_id: 'abc123full', container_name: 'my-app', action: 'pause' },
      })
    })

    const updated = qc.getQueryData<typeof baseDeviceDetail>(['device', 'dev-1'])
    expect(updated!.latest_telemetry.data.docker.containers[0].state).toBe('paused')
  })

  it('sets container state to "running" in cache when action is "unpause"', () => {
    const qc = makeQC()
    const pausedDetail = {
      ...baseDeviceDetail,
      latest_telemetry: {
        ...baseDeviceDetail.latest_telemetry,
        data: {
          docker: { available: true, containers: [{ ...baseContainer, state: 'paused' }] },
        },
      },
    }
    qc.setQueryData(['device', 'dev-1'], pausedDetail)

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    act(() => {
      wsState.handler!({
        type: 'docker_update',
        device_id: 'dev-1',
        data: { container_id: 'abc123full', container_name: 'my-app', action: 'unpause' },
      })
    })

    const updated = qc.getQueryData<typeof baseDeviceDetail>(['device', 'dev-1'])
    expect(updated!.latest_telemetry.data.docker.containers[0].state).toBe('running')
  })

  it('strips leading "/" from container_name before matching (Docker "die" with slash prefix)', () => {
    const qc = makeQC()
    qc.setQueryData(['device', 'dev-1'], baseDeviceDetail)

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    act(() => {
      wsState.handler!({
        type: 'docker_update',
        device_id: 'dev-1',
        data: { container_id: 'abc123full', container_name: '/my-app', action: 'die' },
      })
    })

    const updated = qc.getQueryData<typeof baseDeviceDetail>(['device', 'dev-1'])
    expect(updated!.latest_telemetry.data.docker.containers[0].state).toBe('exited')
  })

  it('does not modify cache for "create" action (no state mapping defined)', () => {
    const qc = makeQC()
    qc.setQueryData(['device', 'dev-1'], baseDeviceDetail)
    const before = qc.getQueryData(['device', 'dev-1'])

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    act(() => {
      wsState.handler!({
        type: 'docker_update',
        device_id: 'dev-1',
        data: { container_id: 'abc123full', container_name: 'my-app', action: 'create' },
      })
    })

    // The setQueryData callback returns old unchanged when there is no state
    // mapping, so the cache reference should be the same object.
    const after = qc.getQueryData(['device', 'dev-1'])
    expect(after).toBe(before)
  })

  it('does not modify cache for "destroy" action (no state mapping defined)', () => {
    const qc = makeQC()
    qc.setQueryData(['device', 'dev-1'], baseDeviceDetail)
    const before = qc.getQueryData(['device', 'dev-1'])

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    act(() => {
      wsState.handler!({
        type: 'docker_update',
        device_id: 'dev-1',
        data: { container_id: 'abc123full', container_name: 'my-app', action: 'destroy' },
      })
    })

    const after = qc.getQueryData(['device', 'dev-1'])
    expect(after).toBe(before)
  })

  it('does not modify device detail cache when container name is not in the list', () => {
    const qc = makeQC()
    qc.setQueryData(['device', 'dev-1'], baseDeviceDetail)
    const before = qc.getQueryData(['device', 'dev-1'])

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    act(() => {
      wsState.handler!({
        type: 'docker_update',
        device_id: 'dev-1',
        data: { container_id: 'xyz999', container_name: 'unknown-container', action: 'stop' },
      })
    })

    const after = qc.getQueryData(['device', 'dev-1'])
    expect(after).toBe(before)
  })
})

// ---------------------------------------------------------------------------
// AC-002, AC-008: docker_update non-significant action (healthcheck) must not
//                  touch the device detail cache at all
// ---------------------------------------------------------------------------

describe('[AC-002] [AC-008] docker_update non-significant action (healthcheck) leaves device detail cache untouched', () => {
  it('does not call setQueryData on device detail key for health_status action', () => {
    const qc = makeQC()
    qc.setQueryData(['device', 'dev-1'], baseDeviceDetail)

    const setDataSpy = vi.spyOn(qc, 'setQueryData')

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    // Reset the spy after hook mount (mount may call setQueryData for other reasons)
    setDataSpy.mockClear()

    act(() => {
      wsState.handler!({
        type: 'docker_update',
        device_id: 'dev-1',
        data: { container_id: 'abc123full', container_name: 'my-app', action: 'health_status' },
      })
    })

    const deviceDetailSetCalls = setDataSpy.mock.calls.filter((args) => {
      const key = args[0]
      return Array.isArray(key) && key[0] === 'device' && key[1] === 'dev-1'
    })
    expect(deviceDetailSetCalls.length, 'setQueryData must not be called on device detail for healthcheck').toBe(0)
  })

  it('does not call invalidateQueries on device detail key for health_status action', () => {
    const qc = makeQC()
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries')

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    act(() => {
      wsState.handler!({
        type: 'docker_update',
        device_id: 'dev-1',
        data: { container_id: 'abc123full', container_name: 'my-app', action: 'health_status' },
      })
    })

    const deviceDetailInvalidateCalls = invalidateSpy.mock.calls.filter((args) => {
      const opts = args[0] as { queryKey?: unknown[] } | undefined
      return Array.isArray(opts?.queryKey) &&
        opts.queryKey[0] === 'device' &&
        opts.queryKey[1] === 'dev-1'
    })
    expect(deviceDetailInvalidateCalls.length, 'invalidateQueries must not be called on device detail for healthcheck').toBe(0)
  })

  it('invalidates fleet list ["devices"] for health_status (regression: fleet list invalidation preserved)', () => {
    const qc = makeQC()
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries')

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    act(() => {
      wsState.handler!({
        type: 'docker_update',
        device_id: 'dev-1',
        data: { container_id: 'abc123full', container_name: 'my-app', action: 'health_status' },
      })
    })

    const fleetCalls = invalidateSpy.mock.calls.filter((args) => {
      const opts = args[0] as { queryKey?: unknown[] } | undefined
      return Array.isArray(opts?.queryKey) && opts.queryKey[0] === 'devices'
    })
    expect(fleetCalls.length, 'fleet list must be invalidated for docker_update events').toBeGreaterThan(0)
  })
})

// ---------------------------------------------------------------------------
// AC-008: Fleet list invalidation is preserved for ALL docker_update events
// ---------------------------------------------------------------------------

describe('[AC-008] Fleet list ["devices"] is invalidated for all docker_update events (significant and non-significant)', () => {
  const allActions = ['start', 'stop', 'die', 'create', 'destroy', 'pause', 'unpause', 'health_status', 'exec_start']

  it('invalidates ["devices"] for every tested action', () => {
    const qc = makeQC()
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries')

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    for (const action of allActions) {
      invalidateSpy.mockClear()
      act(() => {
        wsState.handler!({
          type: 'docker_update',
          device_id: 'dev-1',
          data: { container_id: 'abc123full', container_name: 'my-app', action },
        })
      })

      const fleetCalls = invalidateSpy.mock.calls.filter((args) => {
        const opts = args[0] as { queryKey?: unknown[] } | undefined
        return Array.isArray(opts?.queryKey) && opts.queryKey[0] === 'devices'
      })
      expect(fleetCalls.length, `["devices"] must be invalidated for action "${action}"`).toBeGreaterThan(0)
    }
  })

  it('never calls invalidateQueries with ["device", deviceId] for any docker_update action', () => {
    const qc = makeQC()
    qc.setQueryData(['device', 'dev-1'], baseDeviceDetail)
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries')

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    for (const action of allActions) {
      act(() => {
        wsState.handler!({
          type: 'docker_update',
          device_id: 'dev-1',
          data: { container_id: 'abc123full', container_name: 'my-app', action },
        })
      })
    }

    const deviceDetailCalls = invalidateSpy.mock.calls.filter((args) => {
      const opts = args[0] as { queryKey?: unknown[] } | undefined
      return Array.isArray(opts?.queryKey) &&
        opts.queryKey[0] === 'device' &&
        opts.queryKey[1] === 'dev-1'
    })
    expect(deviceDetailCalls.length, 'invalidateQueries must never target ["device", deviceId]').toBe(0)
  })
})

// ---------------------------------------------------------------------------
// AC-004, AC-005: WS reconnect triggers invalidation of device detail queries
// ---------------------------------------------------------------------------

describe('[AC-004] [AC-005] WS reconnect invalidates all device detail queries', () => {
  it('calls invalidateQueries with queryKey ["device"] (prefix) when connected transitions false→true', () => {
    wsState.connected = false

    const qc = makeQC()
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries')

    const { rerender } = renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    // Reset spy so initial render calls don't interfere
    invalidateSpy.mockClear()

    // Simulate WS reconnecting
    wsState.connected = true
    rerender()

    const reconnectCalls = invalidateSpy.mock.calls.filter((args) => {
      const opts = args[0] as { queryKey?: unknown[]; exact?: boolean } | undefined
      return Array.isArray(opts?.queryKey) &&
        opts.queryKey[0] === 'device' &&
        opts.queryKey.length === 1
    })
    expect(reconnectCalls.length, 'invalidateQueries must be called with ["device"] prefix on reconnect').toBeGreaterThan(0)
  })

  it('does NOT call device detail invalidateQueries on disconnect (true→false transition)', () => {
    wsState.connected = true

    const qc = makeQC()
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries')

    const { rerender } = renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    invalidateSpy.mockClear()

    // Simulate WS disconnecting
    wsState.connected = false
    rerender()

    const deviceCalls = invalidateSpy.mock.calls.filter((args) => {
      const opts = args[0] as { queryKey?: unknown[] } | undefined
      return Array.isArray(opts?.queryKey) &&
        opts.queryKey[0] === 'device' &&
        opts.queryKey.length === 1
    })
    expect(deviceCalls.length, 'no device detail invalidation should fire on disconnect').toBe(0)
  })

  it('does NOT fire device detail invalidation on the initial render (no transition has occurred)', () => {
    wsState.connected = false

    const qc = makeQC()
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries')

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    const deviceCalls = invalidateSpy.mock.calls.filter((args) => {
      const opts = args[0] as { queryKey?: unknown[] } | undefined
      return Array.isArray(opts?.queryKey) &&
        opts.queryKey[0] === 'device' &&
        opts.queryKey.length === 1
    })
    expect(deviceCalls.length, 'no device detail invalidation on initial render').toBe(0)
  })
})

// ---------------------------------------------------------------------------
// AC-003: Telemetry WS updates device detail cache (regression guard)
// ---------------------------------------------------------------------------

describe('[AC-003] Telemetry WS message updates device detail cache via setQueryData', () => {
  it('updates container cpu_percent, mem_usage, and status in device detail cache from telemetry WS push', () => {
    const qc = makeQC()
    qc.setQueryData(['device', 'dev-1'], baseDeviceDetail)

    renderHook(() => useDevices(), { wrapper: makeWrapper(qc) })

    act(() => {
      wsState.handler!({
        type: 'telemetry',
        device_id: 'dev-1',
        data: {
          docker: {
            available: true,
            containers: [
              {
                ...baseContainer,
                cpu_percent: 12.5,
                mem_usage: 75_000_000,
                status: 'Up 3 hours',
              } as ContainerInfo,
            ],
          },
        },
      })
    })

    const updated = qc.getQueryData<typeof baseDeviceDetail>(['device', 'dev-1'])
    const container = updated!.latest_telemetry.data.docker.containers[0]
    expect(container.cpu_percent, 'cpu_percent should be updated from telemetry push').toBe(12.5)
    expect(container.mem_usage, 'mem_usage should be updated from telemetry push').toBe(75_000_000)
    expect(container.status, 'status should be updated from telemetry push').toBe('Up 3 hours')
  })
})

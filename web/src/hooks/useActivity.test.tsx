import { beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import type { ActivityResponse, DeviceActivity } from '../types/models'

const wsState = { connected: true }
vi.mock('./useWebSocket', () => ({
  useWebSocket: () => ({ connected: wsState.connected }),
}))

vi.mock('../api/client', () => ({
  api: { getActivity: vi.fn() },
}))

import { useActivity } from './useActivity'
import { api } from '../api/client'

function makeQC() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function wrapperFor(qc: QueryClient) {
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children)
}

function activity(deviceId: string): DeviceActivity {
  return {
    device_id: deviceId,
    kind: 'host',
    phase: 'in_progress',
    action: 'reboot',
    command_id: `cmd-${deviceId}`,
    actor_phrase: 'by ryan',
    started_at: '2026-10-26T14:00:00Z',
    back_by: null,
    waiting_dpkg_lock: false,
    progress: null,
    agent_from_version: '',
    target: '',
    docker_actions: 0,
    covers_offline: true,
    device_online: true,
    escalation: null,
    powered_off_at: null,
  }
}

function response(devices: string[]): ActivityResponse {
  return { generated_at: '2026-10-26T14:00:00Z', newest_agent_version: '2.78.0', activities: devices.map(activity) }
}

beforeEach(() => {
  vi.clearAllMocks()
  wsState.connected = true
})

describe('[AC-040] the activity query', () => {
  it('fetches /activity once and indexes the activities by device', async () => {
    vi.mocked(api.getActivity).mockResolvedValue(response(['a', 'b']))
    const { result } = renderHook(() => useActivity(), { wrapper: wrapperFor(makeQC()) })

    await waitFor(() => expect(result.current.byDevice.size).toBe(2))
    expect(result.current.byDevice.get('a')?.action).toBe('reboot')
    expect(result.current.newestAgentVersion).toBe('2.78.0')
    expect(api.getActivity).toHaveBeenCalledTimes(1)
  })

  it('exposes an empty map and no version while the first fetch is pending', () => {
    vi.mocked(api.getActivity).mockReturnValue(new Promise(() => {}))
    const { result } = renderHook(() => useActivity(), { wrapper: wrapperFor(makeQC()) })

    expect(result.current.byDevice.size).toBe(0)
    expect(result.current.newestAgentVersion).toBe('')
    expect(result.current.isLoading).toBe(true)
  })

  it('exposes an empty map when the endpoint fails, so pages fall back to the plain status', async () => {
    vi.mocked(api.getActivity).mockRejectedValue(new Error('HTTP 503'))
    const { result } = renderHook(() => useActivity(), { wrapper: wrapperFor(makeQC()) })

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.byDevice.size).toBe(0)
  })

  it('refetches after the WebSocket reconnects, since updates may have been missed', async () => {
    vi.mocked(api.getActivity).mockResolvedValueOnce(response(['a'])).mockResolvedValueOnce(response(['a', 'b']))
    const qc = makeQC()
    const { result, rerender } = renderHook(() => useActivity(), { wrapper: wrapperFor(qc) })
    await waitFor(() => expect(result.current.byDevice.size).toBe(1))

    wsState.connected = false
    rerender()
    expect(api.getActivity, 'going offline alone must not refetch').toHaveBeenCalledTimes(1)

    wsState.connected = true
    rerender()
    await waitFor(() => expect(result.current.byDevice.size).toBe(2))
    expect(api.getActivity).toHaveBeenCalledTimes(2)
  })

  it('does not refetch on the first connect', async () => {
    vi.mocked(api.getActivity).mockResolvedValue(response(['a']))
    const { result, rerender } = renderHook(() => useActivity(), { wrapper: wrapperFor(makeQC()) })
    await waitFor(() => expect(result.current.byDevice.size).toBe(1))

    rerender()
    expect(api.getActivity).toHaveBeenCalledTimes(1)
  })
})

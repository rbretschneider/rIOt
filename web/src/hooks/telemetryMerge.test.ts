import { describe, it, expect, vi, afterEach } from 'vitest'
import type { DeviceDetailResponse, FullTelemetryData } from '../types/models'
import { isStaleTelemetry, mergeTelemetry } from './telemetryMerge'

const T1 = '2026-10-17T10:00:00Z'
const T2 = '2026-10-17T10:00:30Z'
const T3 = '2026-10-17T10:01:00Z'

function makeDetail(timestamp: string = T2, extra: Partial<FullTelemetryData> = {}): DeviceDetailResponse {
  return {
    device: {
      id: 'dev-1',
      short_id: 'dev1',
      hostname: 'host-1',
      arch: 'amd64',
      status: 'online',
      location: '',
      tags: [],
      docker_available: true,
      docker_container_count: 1,
      docker_auto_update_container_count: 0,
      auto_patch: false,
      has_auto_update: false,
      last_telemetry: '2026-10-17T10:00:31Z',
      created_at: T1,
      updated_at: T1,
    },
    latest_telemetry: {
      id: 7,
      device_id: 'dev-1',
      timestamp,
      data: {
        cpu: { usage_percent: 10 },
        processes: { total: 99 },
        ...extra,
      } as unknown as FullTelemetryData,
    },
  }
}

const cpu = (usage: number) => ({ cpu: { usage_percent: usage } }) as unknown as FullTelemetryData

afterEach(() => {
  vi.useRealTimers()
})

describe('[AC-080] an older live snapshot is discarded', () => {
  it('discards a push whose collected_at is before the one already applied', () => {
    const old = makeDetail(T2)
    const result = mergeTelemetry(old, { deviceId: 'dev-1', data: cpu(99), collectedAt: T1, receivedAt: T3 })

    expect(result.stale, 'T1 < T2 must be reported stale').toBe(true)
    expect(result.detail, 'a stale push must return the cache untouched').toBe(old)
    expect(result.detail?.latest_telemetry?.timestamp).toBe(T2)
  })

  it('applies a push that is newer than the one already applied', () => {
    const result = mergeTelemetry(makeDetail(T2), { deviceId: 'dev-1', data: cpu(55), collectedAt: T3, receivedAt: T3 })

    expect(result.stale).toBe(false)
    expect((result.detail?.latest_telemetry?.data as unknown as { cpu: { usage_percent: number } }).cpu.usage_percent).toBe(55)
    expect(result.detail?.latest_telemetry?.timestamp).toBe(T3)
  })

  it('compares instants, not strings (different offsets and fractional seconds)', () => {
    // 10:00:30+02:00 is 08:00:30Z, i.e. older than 10:00:00Z even though the string sorts later.
    expect(isStaleTelemetry('2026-10-17T10:00:00Z', '2026-10-17T10:00:30+02:00')).toBe(true)
    expect(isStaleTelemetry('2026-10-17T10:00:00.5Z', '2026-10-17T10:00:00.25Z')).toBe(true)
    expect(isStaleTelemetry('2026-10-17T10:00:00.25Z', '2026-10-17T10:00:00.5Z')).toBe(false)
  })
})

describe('[AC-081] a message with the same collected_at is discarded', () => {
  it('discards a push whose collected_at equals the latest applied', () => {
    const old = makeDetail(T2)
    const result = mergeTelemetry(old, { deviceId: 'dev-1', data: cpu(77), collectedAt: T2, receivedAt: T3 })

    expect(result.stale, 'equal collected_at is "at or before", so it is stale').toBe(true)
    expect(result.detail).toBe(old)
  })

  it('treats a re-delivered identical message as stale the second time', () => {
    const first = mergeTelemetry(makeDetail(T1), { deviceId: 'dev-1', data: cpu(40), collectedAt: T2, receivedAt: T2 })
    expect(first.stale).toBe(false)
    const second = mergeTelemetry(first.detail, { deviceId: 'dev-1', data: cpu(41), collectedAt: T2, receivedAt: T2 })
    expect(second.stale).toBe(true)
    expect(second.detail).toBe(first.detail)
  })
})

describe('[AC-082] last-seen comes from received_at, never the browser clock', () => {
  it('sets device.last_telemetry to received_at and the snapshot timestamp to collected_at while the browser clock is skewed +1h', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-17T11:00:30Z')) // +1h relative to the server times below

    const result = mergeTelemetry(makeDetail(T1), {
      deviceId: 'dev-1',
      data: cpu(60),
      collectedAt: T2,
      receivedAt: T3,
    })

    expect(result.detail?.device.last_telemetry, 'last-seen must equal received_at').toBe(T3)
    expect(result.detail?.latest_telemetry?.timestamp, 'snapshot time must equal collected_at').toBe(T2)
  })

  it('leaves both stored timestamps unchanged when a pre-lifecycle server sends neither', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2030-01-01T00:00:00Z'))
    const old = makeDetail(T2)

    const result = mergeTelemetry(old, { deviceId: 'dev-1', data: cpu(61) })

    expect(result.stale, 'without collected_at the push cannot be ordered, so it applies').toBe(false)
    expect(result.detail?.device.last_telemetry).toBe(old.device.last_telemetry)
    expect(result.detail?.latest_telemetry?.timestamp).toBe(T2)
  })

  it('keeps last_telemetry unchanged when only collected_at is present', () => {
    const old = makeDetail(T1)
    const result = mergeTelemetry(old, { deviceId: 'dev-1', data: cpu(62), collectedAt: T3 })
    expect(result.detail?.device.last_telemetry).toBe(old.device.last_telemetry)
    expect(result.detail?.latest_telemetry?.timestamp).toBe(T3)
  })
})

describe('telemetry merge keeps the data the broadcast strips', () => {
  it('keeps fields the push omits and overwrites fields it carries', () => {
    const result = mergeTelemetry(makeDetail(T1), { deviceId: 'dev-1', data: cpu(33), collectedAt: T2, receivedAt: T2 })
    const data = result.detail?.latest_telemetry?.data as unknown as { cpu: { usage_percent: number }; processes: { total: number } }
    expect(data.cpu.usage_percent).toBe(33)
    expect(data.processes.total, 'heavy fields stripped from the broadcast must survive').toBe(99)
  })

  it('does not overwrite a field with null', () => {
    const data = { cpu: null } as unknown as FullTelemetryData
    const result = mergeTelemetry(makeDetail(T1), { deviceId: 'dev-1', data, collectedAt: T2, receivedAt: T2 })
    const merged = result.detail?.latest_telemetry?.data as unknown as { cpu: { usage_percent: number } }
    expect(merged.cpu.usage_percent).toBe(10)
  })

  it('marks the device as warning when the UPS is on battery', () => {
    const data = { ups: { on_battery: true } } as unknown as FullTelemetryData
    const result = mergeTelemetry(makeDetail(T1), { deviceId: 'dev-1', data, collectedAt: T2, receivedAt: T2 })
    expect(result.detail?.device.status).toBe('warning')
  })

  it('does nothing when no device detail is cached', () => {
    const result = mergeTelemetry(undefined, { deviceId: 'dev-1', data: cpu(1), collectedAt: T2, receivedAt: T2 })
    expect(result).toEqual({ detail: undefined, stale: false })
  })

  it('applies a push to a cached detail that has no telemetry yet', () => {
    const old: DeviceDetailResponse = { ...makeDetail(T1), latest_telemetry: undefined }
    const result = mergeTelemetry(old, { deviceId: 'dev-1', data: cpu(5), collectedAt: T2, receivedAt: T3 })
    expect(result.stale).toBe(false)
    expect(result.detail?.latest_telemetry?.timestamp).toBe(T2)
    expect(result.detail?.device.last_telemetry).toBe(T3)
  })
})

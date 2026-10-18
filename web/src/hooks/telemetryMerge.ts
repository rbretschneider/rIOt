// CMD-LIFECYCLE-001 (AD-018, FR-066..068): pure merge of a live telemetry push
// into the cached device detail.
//
// Two rules matter here:
//   1. The dashboard never moves backward in time. A push whose collected_at is
//      at or before the latest one already applied is discarded.
//   2. Timestamps come from the server (collected_at, received_at), never from
//      the browser clock. When the server omits one (a pre-lifecycle server),
//      the stored value is left unchanged.
//
// No clock is read in this file, so the behaviour is fully deterministic.

import type { DeviceDetailResponse, FullTelemetryData, TelemetrySnapshot } from '../types/models'

export interface TelemetryPush {
  deviceId: string
  data: FullTelemetryData
  /** Agent time of the snapshot, clamped by the server. Absent from an old server. */
  collectedAt?: string
  /** Server time the snapshot arrived. Absent from an old server. */
  receivedAt?: string
}

export interface TelemetryMergeResult {
  detail: DeviceDetailResponse | undefined
  /** True when the push was discarded as not newer than what is already applied. */
  stale: boolean
}

function toMillis(iso: string | undefined): number {
  if (!iso) return Number.NaN
  return Date.parse(iso)
}

/**
 * True when `collectedAt` is at or before `lastCollectedAt`. Compared as
 * instants, not strings, because RFC3339 values can differ in offset and
 * fractional digits. If either side is missing or unparseable the push cannot
 * be ordered, so it is not stale.
 */
export function isStaleTelemetry(lastCollectedAt: string | undefined, collectedAt: string | undefined): boolean {
  const next = toMillis(collectedAt)
  const last = toMillis(lastCollectedAt)
  if (Number.isNaN(next) || Number.isNaN(last)) return false
  return next <= last
}

/**
 * Applies a telemetry push to the cached device detail.
 *
 * The server strips heavy fields (processes, hardware, USB, etc.) from live
 * broadcasts to save bandwidth, so fields the push omits are kept from the
 * prior fetch. A stale push returns the cache unchanged (same reference).
 */
export function mergeTelemetry(old: DeviceDetailResponse | undefined, push: TelemetryPush): TelemetryMergeResult {
  if (!old) return { detail: old, stale: false }

  const previous = old.latest_telemetry
  if (isStaleTelemetry(previous?.timestamp, push.collectedAt)) {
    return { detail: old, stale: true }
  }

  const merged: Record<string, unknown> = { ...previous?.data }
  for (const [key, value] of Object.entries(push.data ?? {})) {
    if (value != null) {
      merged[key] = value
    }
  }

  const upsOnBattery = push.data?.ups?.on_battery === true
  const snapshot: TelemetrySnapshot = {
    id: previous?.id ?? 0,
    device_id: push.deviceId,
    timestamp: push.collectedAt ?? previous?.timestamp ?? push.receivedAt ?? '',
    data: merged as FullTelemetryData,
  }

  return {
    detail: {
      ...old,
      device: {
        ...old.device,
        ...(upsOnBattery ? { status: 'warning' as const } : {}),
        ...(push.receivedAt ? { last_telemetry: push.receivedAt } : {}),
      },
      latest_telemetry: snapshot,
    },
    stale: false,
  }
}

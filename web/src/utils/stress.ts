import type { HeartbeatData } from '../types/models'

/**
 * Computes the fleet "stress score" for a device per FR-034 / BR-004:
 *
 *   score = 0.4 * cpu_percent
 *         + 0.3 * mem_percent
 *         + 0.2 * disk_root_percent
 *         + 0.1 * min(load_avg_1m * 25, 100)
 *
 * Offline devices return Number.NEGATIVE_INFINITY so they sort to the end
 * of the heatmap grid regardless of their last-known values (AC-016).
 *
 * @param hb   - The most recent HeartbeatData for the device.
 * @param online - Whether the device is currently online.
 */
export function stressScore(hb: HeartbeatData | undefined, online: boolean): number {
  if (!online) {
    return Number.NEGATIVE_INFINITY
  }

  if (!hb) {
    return 0
  }

  const cpu = hb.cpu_percent ?? 0
  const mem = hb.mem_percent ?? 0
  const disk = hb.disk_root_percent ?? 0
  const load = Math.min((hb.load_avg_1m ?? 0) * 25, 100)

  return 0.4 * cpu + 0.3 * mem + 0.2 * disk + 0.1 * load
}

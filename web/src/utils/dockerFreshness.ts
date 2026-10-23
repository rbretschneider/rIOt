// Image freshness of one container, from the server's update verdict. Agents
// that predate update_status only send the older true/false field, which is
// used when the verdict is absent (the same rule as the Update badge).
export type Freshness = 'out_of_date' | 'up_to_date' | 'pinned' | 'unknown'

export const FRESHNESS_LABEL: Record<Freshness, string> = {
  out_of_date: 'Out of date',
  up_to_date: 'Up to date',
  pinned: 'Pinned',
  unknown: 'Unknown',
}

export function freshnessOf(r: {
  update_status?: string
  update_available?: boolean | null
}): Freshness {
  if (r.update_status) {
    switch (r.update_status) {
      case 'update_available':
        return 'out_of_date'
      case 'up_to_date':
        return 'up_to_date'
      case 'pinned':
        return 'pinned'
      default:
        return 'unknown'
    }
  }
  if (r.update_available === true) return 'out_of_date'
  if (r.update_available === false) return 'up_to_date'
  return 'unknown'
}

/** The "Out of date" filter. pinned and failed checks never match. */
export const isOutOfDate = (r: Parameters<typeof freshnessOf>[0]) =>
  freshnessOf(r) === 'out_of_date'

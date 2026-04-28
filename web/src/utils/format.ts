/** Human-readable byte rate. Picks the unit so values stay in [0, 1024). */
export function formatBytesPerSec(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '0 B/s'
  if (bytes < 1024) return `${bytes.toFixed(0)} B/s`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB/s`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB/s`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB/s`
}

/** Compact byte rate for chart axis ticks ("3K", "1.2M") — no unit suffix, just magnitude. */
export function formatBytesPerSecAxis(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 1) return '0'
  if (bytes < 1024) return bytes.toFixed(0)
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)}K`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)}M`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)}G`
}

/** Percent value without trailing decimals when the integer form is unambiguous.
 * 100.0 → "100%", 99.95 → "100%", 0.7 → "0.7%", 12.34 → "12%". */
export function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return '—'
  if (value >= 10) return `${Math.round(value)}%`
  return `${value.toFixed(1)}%`
}

/** Plain integer count, never decimals. */
export function formatCount(value: number): string {
  if (!Number.isFinite(value)) return '—'
  return Math.round(value).toString()
}

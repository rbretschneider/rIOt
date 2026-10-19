export interface PaletteEntry {
  label: string
  kind: 'Host' | 'Container' | 'Stack' | 'Page'
  path: string
  host?: string
  search: string
  offline?: boolean
}
export function paletteRank(entries: PaletteEntry[], query: string) {
  const q = query.trim().toLowerCase()
  const tokens = q.split(/\s+/).filter(Boolean)
  const order = { Host: 0, Container: 1, Stack: 2, Page: 3 }
  const rank = (r: PaletteEntry) =>
    r.label.toLowerCase().startsWith(q)
      ? 0
      : r.label.toLowerCase().includes(q)
        ? 1
        : 2
  return entries
    .filter((e) => tokens.every((t) => e.search.toLowerCase().includes(t)))
    .sort(
      (a, b) =>
        order[a.kind] - order[b.kind] ||
        rank(a) - rank(b) ||
        a.label.localeCompare(b.label) ||
        (a.host ?? '').localeCompare(b.host ?? ''),
    )
}

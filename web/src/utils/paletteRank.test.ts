import { expect, it } from 'vitest'
import { paletteRank, type PaletteEntry } from './paletteRank'
it('matches every token and groups hosts, containers, stacks and pages', () => {
  const entries: PaletteEntry[] = [
    {
      kind: 'Container',
      label: 'web',
      host: 'prod',
      search: 'web prod nginx app',
      path: 'a',
    },
    { kind: 'Host', label: 'prod-web', search: 'prod web', path: 'b' },
    { kind: 'Stack', label: 'app', search: 'app prod web', path: 'c' },
  ]
  expect(paletteRank(entries, 'WEB prod').map((r) => r.path)).toEqual([
    'b',
    'a',
    'c',
  ])
  expect(paletteRank(entries, 'web staging')).toEqual([])
})
it('ranks prefix before name substring before other fields', () => {
  const rows: PaletteEntry[] = [
    {
      kind: 'Container',
      label: 'database',
      search: 'database web',
      path: 'other',
    },
    { kind: 'Container', label: 'my-web', search: 'my-web', path: 'contains' },
    { kind: 'Container', label: 'web', search: 'web', path: 'prefix' },
  ]
  expect(paletteRank(rows, 'web').map((r) => r.path)).toEqual([
    'prefix',
    'contains',
    'other',
  ])
})
it('ranks five thousand entries within the palette responsiveness budget', () => {
  const entries: PaletteEntry[] = Array.from({ length: 5000 }, (_, i) => ({
    label: `web-${i}`,
    kind: 'Container',
    path: `/${i}`,
    search: `web-${i} host nginx app`,
  }))
  const start = performance.now()
  expect(paletteRank(entries, 'web host').length).toBe(5000)
  expect(performance.now() - start).toBeLessThan(50)
})

import { describe, expect, it } from 'vitest'

// Source-level guards for UI-FOUNDATION-001. They read every TypeScript source file
// under web/src as raw text (no fs access needed) and fail when a convention the
// story established is broken again.

const GUARD_FILE = '../test/static-guards.test.ts'

const allSources = import.meta.glob('../**/*.{ts,tsx}', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

const isTest = (path: string) => /\.test\.tsx?$/.test(path)

/** Non-test sources, keyed by the path relative to web/src (for example "components/ui/Button.tsx"). */
const sources: Record<string, string> = {}
const tests: Record<string, string> = {}
for (const [key, text] of Object.entries(allSources)) {
  if (key === GUARD_FILE) continue
  const path = key.replace(/^\.\.\//, '')
  if (isTest(path)) tests[path] = text
  else sources[path] = text
}

function stripComments(text: string): string {
  return text.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:'"`])\/\/.*$/gm, '$1')
}

/** Text between two markers, for files where only one component is under guard. */
function between(text: string, start: string, end: string): string {
  const a = text.indexOf(start)
  const b = text.indexOf(end, a)
  return a >= 0 && b > a ? text.slice(a, b) : ''
}

function offenders(files: Record<string, string>, pattern: RegExp, prepare: (t: string) => string = t => t): string[] {
  return Object.entries(files)
    .filter(([, text]) => pattern.test(prepare(text)))
    .map(([path]) => path)
}

const HEX = /#[0-9a-fA-F]{3,8}\b/
/** Hex scan input: gradient references (url(#id)) and HTML entities (&#123;) are not colors. */
const forHexScan = (text: string) => stripComments(text).replace(/url\(#[^)]*\)/g, '').replace(/&#\w+;?/g, '')

describe('[UIF AC-004] no hex literals in chart and touched files', () => {
  const guarded = [
    'components/MetricChart.tsx',
    'components/ContainerMetricChart.tsx',
    'pages/dashboard/PerDeviceCharts.tsx',
    'components/ContainerDetail.tsx',
    'pages/Alerts.tsx',
    'pages/DeviceDetail.tsx',
  ]

  it.each(guarded)('%s exists in the scan set', path => {
    expect(sources[path], `${path} was not found by the source glob`).toBeDefined()
  })

  it.each(guarded)('%s contains no #rrggbb color literal', path => {
    expect(forHexScan(sources[path])).not.toMatch(HEX)
  })

  it('does not scan Terminal.tsx (xterm theme is exempt) and the exemption is deliberate', () => {
    expect(guarded).not.toContain('components/Terminal.tsx')
  })

  it('the shared chart color source lives under theme/, not components/ui', () => {
    expect(sources['theme/chartColors.ts']).toBeDefined()
    expect(forHexScan(sources['theme/chartColors.ts'])).toMatch(HEX)
  })
})

describe('[UIF AC-005] components/ui uses tokens only', () => {
  const ui = Object.fromEntries(Object.entries(sources).filter(([path]) => path.startsWith('components/ui/')))

  it('finds the primitive library', () => {
    expect(Object.keys(ui).length).toBeGreaterThanOrEqual(12)
  })

  it('has no raw gray-N palette class', () => {
    expect(offenders(ui, /\bgray-\d/, stripComments)).toEqual([])
  })

  it('has no hex color literal', () => {
    expect(offenders(ui, HEX, forHexScan)).toEqual([])
  })

  it('has no hard-coded z-index utility: layers come from the shared scale', () => {
    expect(offenders(ui, /\bz-(\d+|\[\d+\])\b/, stripComments)).toEqual([])
  })
})

describe('[UIF AC-031] migrated modals carry no ad-hoc z-index', () => {
  const AD_HOC_Z = /\bz-(40|50|\[60\])\b/

  const wholeFile = [
    'components/ConfirmModal.tsx',
    'components/UpdateConfirmModal.tsx',
    'components/ActivityLog.tsx',
    'pages/settings/AlertRuleSettings.tsx',
    'pages/settings/NotificationSettings.tsx',
  ]

  it.each(wholeFile)('%s has no z-40, z-50 or z-[60]', path => {
    expect(sources[path], `${path} was not found`).toBeDefined()
    expect(stripComments(sources[path])).not.toMatch(AD_HOC_Z)
  })

  it('FleetOverview PatchReviewModal has no ad-hoc z-index', () => {
    const section = between(sources['pages/FleetOverview.tsx'], 'function PatchReviewModal', 'function StatCard')
    expect(section.length).toBeGreaterThan(0)
    expect(section).not.toMatch(AD_HOC_Z)
  })

  it('App.tsx UpdateBanner modal has no ad-hoc z-index', () => {
    const section = between(sources['App.tsx'], 'function UpdateBanner', 'function ServerVersion')
    expect(section.length).toBeGreaterThan(0)
    expect(section).not.toMatch(AD_HOC_Z)
  })

  it('the Tooltip sits on the shared tooltip layer', () => {
    expect(sources['components/Tooltip.tsx']).toContain('z-(--z-tooltip)')
    expect(sources['components/Tooltip.tsx']).not.toContain('z-[100]')
  })
})

describe('[UIF AC-063] no native dialogs in source', () => {
  // (?<![\w.]) keeps onConfirm(, setConfirm...( and .confirm( from matching.
  const NATIVE = /(?<![\w.$])(?:window\.)?(?:confirm|alert|prompt)\s*\(/

  it('finds a meaningful number of source files', () => {
    expect(Object.keys(sources).length).toBeGreaterThan(80)
  })

  it('no non-test file calls confirm(), alert() or prompt()', () => {
    expect(offenders(sources, NATIVE, stripComments)).toEqual([])
  })

  it('no non-test file references window.confirm, window.alert or window.prompt', () => {
    expect(offenders(sources, /window\.(confirm|alert|prompt)\b/, stripComments)).toEqual([])
  })

  it('the pattern really catches the call shapes it exists to ban', () => {
    expect(NATIVE.test("if (confirm('Delete?')) go()")).toBe(true)
    expect(NATIVE.test("window.alert('x')")).toBe(true)
    expect(NATIVE.test('onConfirm()')).toBe(false)
    expect(NATIVE.test('setConfirmAction(null)')).toBe(false)
    expect(NATIVE.test('dialog.confirm(x)')).toBe(false)
  })
})

describe('[UIF AC-064] tests do not stub native dialogs', () => {
  it('finds the test suite', () => {
    expect(Object.keys(tests).length).toBeGreaterThan(30)
  })

  it('no test stubs or replaces window.confirm, window.alert or window.prompt', () => {
    const forbidden = [
      /stubGlobal\(\s*['"](confirm|alert|prompt)['"]/,
      /window\.(confirm|alert|prompt)\s*=[^=]/,
      /spyOn\(\s*window\s*,\s*['"](confirm|prompt)['"]/,
    ]
    for (const pattern of forbidden) {
      expect(offenders(tests, pattern), `tests matching ${pattern}`).toEqual([])
    }
  })
})

describe('[UIF AC-108] no duplicate formatter definitions', () => {
  it('formatBytes, formatUptime and formatDuration are defined only in utils/format.ts', () => {
    const others = Object.fromEntries(Object.entries(sources).filter(([path]) => path !== 'utils/format.ts'))
    const definition = /(?:function\s+|const\s+)(formatBytes|formatUptime|formatDuration)\b/
    expect(offenders(others, definition, stripComments)).toEqual([])
  })

  it('utils/format.ts defines them', () => {
    expect(sources['utils/format.ts']).toMatch(/export function formatBytes\b/)
    expect(sources['utils/format.ts']).toMatch(/export function formatDuration\b/)
  })

  it('utils/docker.ts only re-exports from the canonical module', () => {
    expect(sources['utils/docker.ts']).toMatch(/export \{ formatBytes \} from '\.\/format'/)
  })
})

describe('[UIF AC-110] no Date toLocale* calls in source', () => {
  it('no non-test file formats a date with toLocaleString, toLocaleDateString or toLocaleTimeString', () => {
    expect(offenders(sources, /\.toLocale(Date|Time)?String\(/, stripComments)).toEqual([])
  })
})

describe('[UIF AC-120] dead components removed and unimported', () => {
  const dead = ['Sparkline', 'MiniScore', 'ContainerGroup', 'ContainerCard']

  it.each(dead)('components/%s.tsx no longer exists', name => {
    expect(sources[`components/${name}.tsx`]).toBeUndefined()
  })

  it.each(dead)('nothing imports %s', name => {
    const importer = new RegExp(`from\\s+['"][./]+(?:.*/)?${name}['"]`)
    expect(offenders({ ...sources, ...tests }, importer)).toEqual([])
  })

  it('formatContainerUptime is gone', () => {
    expect(offenders(sources, /formatContainerUptime/)).toEqual([])
  })
})

describe('[UIF AC-121] ContainerDetail has no default export', () => {
  it('removes the slide-out drawer but keeps ContainerDetailContent', () => {
    const text = stripComments(sources['components/ContainerDetail.tsx'])
    expect(text).not.toMatch(/export\s+default\b/)
    expect(text).toMatch(/export function ContainerDetailContent\b/)
  })

  it('no file imports ContainerDetail as a default export', () => {
    const defaultImport = /import\s+ContainerDetail\s+from|default as ContainerDetail\b/
    expect(offenders({ ...sources, ...tests }, defaultImport)).toEqual([])
  })
})

describe('[UIF SEC-005] no raw HTML sinks in source', () => {
  it('bans dangerouslySetInnerHTML, innerHTML assignment and insertAdjacentHTML in non-test source', () => {
    const sink = /dangerouslySetInnerHTML|\.(inner|outer)HTML\s*=[^=]|insertAdjacentHTML/
    expect(offenders(sources, sink, stripComments)).toEqual([])
  })

  it('the pattern really catches those sinks', () => {
    const sink = /dangerouslySetInnerHTML|\.(inner|outer)HTML\s*=[^=]|insertAdjacentHTML/
    expect(sink.test('<div dangerouslySetInnerHTML={{ __html: x }} />')).toBe(true)
    expect(sink.test('el.innerHTML = value')).toBe(true)
    expect(sink.test('el.innerHTML === value')).toBe(false)
  })
})

describe('[UIF SEC-008] demo modules stay out of the production graph', () => {
  const demoImport = /(?:from\s+|import\s*\(\s*)['"][^'"]*(?:demo-client|demo-settings|demo-data|DemoWebSocketProvider)['"]/
  const allowedImporters = (path: string) => /^api\/demo-[\w-]+\.ts$/.test(path) || path === 'contexts/DemoWebSocketProvider.tsx'

  it('only the demo modules themselves import demo-client, demo-settings, demo-data or DemoWebSocketProvider', () => {
    const outside = Object.fromEntries(Object.entries(sources).filter(([path]) => !allowedImporters(path)))
    expect(offenders(outside, demoImport, stripComments)).toEqual([])
  })

  it('finds the demo modules it protects', () => {
    expect(sources['api/demo-client.ts']).toBeDefined()
    expect(sources['api/demo-settings.ts']).toBeDefined()
    expect(sources['api/demo-data.ts']).toBeDefined()
    expect(sources['contexts/DemoWebSocketProvider.tsx']).toBeDefined()
  })

  it('the pattern really catches static and dynamic imports', () => {
    expect(demoImport.test("import { demo } from '../api/demo-data'")).toBe(true)
    expect(demoImport.test("const m = await import('./api/demo-client')")).toBe(true)
    expect(demoImport.test("import { api } from './api/client'")).toBe(false)
  })
})
// ---------------------------------------------------------------------------
// TIME-12H-001 guards. Test-support modules in test/ (keyed as ./x here) are not production code: they may name
// localTimeZone because they exist to drive the provider's test seam.
// ---------------------------------------------------------------------------

const productionSources: Record<string, string> = Object.fromEntries(
  Object.entries(sources).filter(([path]) => !path.startsWith('test/') && !path.startsWith('./')),
)

const FORMAT_FILE = 'utils/format.ts'
const PROVIDER_FILE = 'contexts/TimePreferenceProvider.tsx'

function without(files: Record<string, string>, ...paths: string[]): Record<string, string> {
  return Object.fromEntries(Object.entries(files).filter(([path]) => !paths.includes(path)))
}

describe('TIME-12H-001 [AC-020] only utils/format.ts constructs a zoned Intl.DateTimeFormat', () => {
  const construction = /Intl\.DateTimeFormat\(/

  it('Intl.DateTimeFormat( appears in non-test source only in utils/format.ts and AgentManagement.tsx', () => {
    expect(offenders(productionSources, construction, stripComments).sort()).toEqual([
      'pages/settings/AgentManagement.tsx',
      FORMAT_FILE,
    ])
  })

  it('the AgentManagement construction passes no timeZone (DEC-11: it stays browser-local)', () => {
    const text = stripComments(sources['pages/settings/AgentManagement.tsx'])
    const calls = Array.from(text.matchAll(/Intl\.DateTimeFormat\(([^)]*)\)/g))
    expect(calls.length).toBeGreaterThan(0)
    for (const call of calls) expect(call[1]).not.toMatch(/\btimeZone\s*:/)
  })

  it('the pattern really catches a construction and ignores a type reference', () => {
    expect(construction.test("new Intl.DateTimeFormat('en-US', { timeZone: z })")).toBe(true)
    expect(construction.test('const cache = new Map<string, Intl.DateTimeFormat>()')).toBe(false)
  })
})

describe('TIME-12H-001 [SEC-003] the AcceptedZone brand and the localTimeZone seam cannot be bypassed', () => {
  // (a) a type assertion to AcceptedZone, in either spelling
  const BRAND_CAST = /\bas\s+(?:unknown\s+as\s+)?AcceptedZone\b|<AcceptedZone>\s*[\w(]/
  // (b) the identifier of the unbranded zone input
  const SEAM_NAME = /\blocalTimeZone\b/
  // (c) setting it as a prop or attribute
  const SEAM_ASSIGNED = /\blocalTimeZone\s*=/
  const SEAM_JSX_ATTRIBUTE = /\blocalTimeZone=[{"']/

  it('a type assertion to AcceptedZone appears in production source only in utils/format.ts', () => {
    expect(offenders(without(productionSources, FORMAT_FILE), BRAND_CAST, stripComments)).toEqual([])
  })

  it('utils/format.ts has exactly one such assertion: the return of acceptServerZone', () => {
    const matches = stripComments(sources[FORMAT_FILE]).match(new RegExp(BRAND_CAST, 'g')) ?? []
    expect(matches).toHaveLength(1)
    expect(between(sources[FORMAT_FILE], 'export function acceptServerZone', 'export function localZoneName')).toMatch(
      /return raw as AcceptedZone/,
    )
  })

  it('localTimeZone is named in production source only by the provider and utils/format.ts', () => {
    expect(offenders(without(productionSources, PROVIDER_FILE, FORMAT_FILE), SEAM_NAME, stripComments)).toEqual([])
  })

  it('no production .tsx file other than the provider assigns localTimeZone, and the provider never sets it as a JSX attribute', () => {
    const tsx = Object.fromEntries(Object.entries(productionSources).filter(([path]) => path.endsWith('.tsx') && path !== PROVIDER_FILE))
    expect(offenders(tsx, SEAM_ASSIGNED, stripComments)).toEqual([])
    expect(stripComments(sources[PROVIDER_FILE])).not.toMatch(SEAM_JSX_ATTRIBUTE)
  })

  it('the patterns really catch x as AcceptedZone, x as unknown as AcceptedZone and a localTimeZone prop', () => {
    expect(BRAND_CAST.test('const z = raw as AcceptedZone')).toBe(true)
    expect(BRAND_CAST.test('const z = raw as unknown as AcceptedZone')).toBe(true)
    expect(BRAND_CAST.test('const z = <AcceptedZone>raw')).toBe(true)
    expect(SEAM_ASSIGNED.test('<TimePreferenceProvider localTimeZone="Mars/Olympus">')).toBe(true)
    expect(SEAM_JSX_ATTRIBUTE.test('<TimePreferenceProvider localTimeZone="Mars/Olympus">')).toBe(true)
    expect(SEAM_JSX_ATTRIBUTE.test('<TimePreferenceProvider localTimeZone={data.zone}>')).toBe(true)
    expect(SEAM_NAME.test('const { localTimeZone } = props')).toBe(true)
  })

  it('the patterns do not match the type declaration or ordinary uses of the type', () => {
    expect(BRAND_CAST.test("export type AcceptedZone = string & { readonly __brand: 'AcceptedZone' }")).toBe(false)
    expect(BRAND_CAST.test('serverZone: AcceptedZone | null')).toBe(false)
    expect(BRAND_CAST.test('const has = true')).toBe(false)
    expect(SEAM_NAME.test('const { localZoneName } = useTimePreference()')).toBe(false)
  })
})

/** Number of top-level arguments of the call whose opening parenthesis is at `open`, or -1 when unbalanced. */
function topLevelArgumentCount(text: string, open: number): number {
  let depth = 0
  let args = 0
  let quote = ''
  let sawContent = false
  for (let i = open; i < text.length; i++) {
    const ch = text[i]
    if (quote) {
      if (ch === '\\') i++
      else if (ch === quote) quote = ''
      continue
    }
    if (ch === "'" || ch === '"' || ch === '`') {
      quote = ch
      sawContent = true
      continue
    }
    if (ch === '(' || ch === '[' || ch === '{') {
      depth++
      if (depth > 1) sawContent = true
      continue
    }
    if (ch === ')' || ch === ']' || ch === '}') {
      depth--
      if (depth === 0) return sawContent ? args + 1 : 0
      continue
    }
    if (ch === ',' && depth === 1) {
      args++
      continue
    }
    if (!/\s/.test(ch)) sawContent = true
  }
  return -1
}

function callArgumentCounts(text: string, name: string): number[] {
  const counts: number[] = []
  const call = new RegExp(`(?<![\\w.$])${name}\\s*\\(`, 'g')
  for (const match of text.matchAll(call)) {
    if (/function\s+$/.test(text.slice(0, match.index))) continue
    counts.push(topLevelArgumentCount(text, match.index + match[0].length - 1))
  }
  return counts
}

describe('TIME-12H-001 [AC-036] production calls pass the preference to formatRunDate and parseSystemdCalendar', () => {
  const callers = without(productionSources, 'utils/cron.ts')

  it('every formatRunDate( call outside utils/cron.ts has three top-level arguments', () => {
    const counts = Object.entries(callers).flatMap(([path, text]) =>
      callArgumentCounts(stripComments(text), 'formatRunDate').map(count => ({ path, count })),
    )
    expect(counts.length, 'the guard found no formatRunDate call to check').toBeGreaterThan(0)
    expect(counts.filter(c => c.count !== 3)).toEqual([])
  })

  it('every parseSystemdCalendar( call outside utils/cron.ts has two top-level arguments', () => {
    const counts = Object.entries(callers).flatMap(([path, text]) =>
      callArgumentCounts(stripComments(text), 'parseSystemdCalendar').map(count => ({ path, count })),
    )
    expect(counts.length, 'the guard found no parseSystemdCalendar call to check').toBeGreaterThan(0)
    expect(counts.filter(c => c.count !== 2)).toEqual([])
  })

  it('the argument counter handles nesting, strings and empty calls', () => {
    expect(callArgumentCounts("formatRunDate(timer.next_run, '-', zoneOpts)", 'formatRunDate')).toEqual([3])
    expect(callArgumentCounts('formatRunDate(value)', 'formatRunDate')).toEqual([1])
    expect(callArgumentCounts("formatRunDate(a, '-, -', { zone: 'UTC', hourCycle: '24h' })", 'formatRunDate')).toEqual([3])
    expect(callArgumentCounts('parseSystemdCalendar(pick(a, b), cycle)', 'parseSystemdCalendar')).toEqual([2])
    expect(callArgumentCounts('parseSystemdCalendar()', 'parseSystemdCalendar')).toEqual([0])
    expect(callArgumentCounts('export function formatRunDate(value: string)', 'formatRunDate')).toEqual([])
    expect(callArgumentCounts('const x = myformatRunDate(a)', 'formatRunDate')).toEqual([])
  })
})

describe('TIME-12H-001 [FR-047] only TimePreferenceProvider names the time storage keys', () => {
  it('the riot-time-zone and riot-hour-cycle literals appear in no other production source', () => {
    expect(offenders(without(productionSources, PROVIDER_FILE), /riot-time-zone|riot-hour-cycle/, stripComments)).toEqual([])
  })

  it('the provider defines both keys', () => {
    expect(sources[PROVIDER_FILE]).toMatch(/TIME_ZONE_STORAGE_KEY = 'riot-time-zone'/)
    expect(sources[PROVIDER_FILE]).toMatch(/HOUR_CYCLE_STORAGE_KEY = 'riot-hour-cycle'/)
  })
})

describe('TIME-12H-001 [FR-062] activity labels do not use the chart tick formatter', () => {
  it.each(['components/ActivityLabel.tsx', 'components/AgentUpdatingVersion.tsx'])('%s does not reference formatChartTick', path => {
    expect(sources[path], `${path} was not found`).toBeDefined()
    expect(stripComments(sources[path])).not.toMatch(/formatChartTick/)
  })
})

describe('TIME-12H-001 [BR-005] the provider is mounted at the root and the zone sync in the authenticated app', () => {
  it('main.tsx mounts <TimePreferenceProvider>', () => {
    expect(stripComments(sources['main.tsx'])).toContain('<TimePreferenceProvider>')
  })

  it('App.tsx mounts <ServerZoneSync /> after the login and setup returns, so it never runs before login', () => {
    const app = stripComments(sources['App.tsx'])
    const sync = app.indexOf('<ServerZoneSync />')
    expect(sync, 'App.tsx does not mount <ServerZoneSync />').toBeGreaterThan(-1)
    expect(sync).toBeGreaterThan(app.indexOf('<Login onLogin={login} />'))
    expect(app.match(/<ServerZoneSync \/>/g)).toHaveLength(1)
  })
})

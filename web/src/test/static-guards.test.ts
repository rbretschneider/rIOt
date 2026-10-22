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

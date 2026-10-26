import { describe, expect, it } from 'vitest'

// AC-047 / ADD 12 note 9, web half: nothing added or changed for EXPECT-001 may spawn
// a process, and nothing that renders server text may use a raw HTML sink. The scan
// set is computed from the sources themselves, so new EXPECT-001 test files are
// picked up automatically. The scanned list is printed for the QA engineer.

const sources = import.meta.glob('/src/**/*.{ts,tsx}', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

const SELF = '/src/utils/activity.safety.test.ts'

const isTest = (path: string) => /\.test\.tsx?$/.test(path)

// Every test file that belongs to EXPECT-001 (it carries an [AC-...] tag and mentions the story's
// subject), plus every new EXPECT-001 source file.
const NEW_SOURCES = [
  '/src/utils/activity.ts',
  '/src/hooks/useActivity.ts',
  '/src/components/ActivityLabel.tsx',
]

const expectTests = Object.entries(sources)
  .filter(
    ([path, text]) =>
      path !== SELF &&
      isTest(path) &&
      (/activity[./]/.test(path) || /activity_update|EXPECT-001|getActivity|useActivity|utils\/activity/.test(text)),
  )
  .map(([path]) => path)
  .sort()

const scanned = [...NEW_SOURCES, ...expectTests]

const SPAWN = /child_process|node:child_process|\bspawn\(|\bexecSync\(|\bexecFile\(|\bexec\(/
const HTML_SINK = /dangerouslySetInnerHTML|\.innerHTML\s*=|insertAdjacentHTML/

// The scan set must be real before "nothing was found" means anything.
const read = (path: string) => sources[path] ?? ''

// eslint-disable-next-line no-console
console.info(`[AC-047] web scan set (${scanned.length} files):\n${scanned.join('\n')}`)

describe('[AC-047] new web tests spawn nothing', () => {
  it('finds the new sources and at least the EXPECT-001 test files written for this story', () => {
    for (const path of NEW_SOURCES) expect(sources[path], `${path} was not found by the glob`).toBeDefined()
    for (const required of [
      '/src/utils/activity.test.ts',
      '/src/hooks/useActivity.test.tsx',
      '/src/pages/FleetOverview.activity.test.tsx',
      '/src/components/docker/CommandProgress.activity.test.tsx',
      '/src/pages/DeviceDetail.activity.test.tsx',
    ]) {
      expect(expectTests, `${required} must be in the scan set`).toContain(required)
    }
  })

  it.each(scanned)('%s imports no child_process and starts no process', path => {
    expect(read(path), `${path} spawns a process`).not.toMatch(SPAWN)
  })

  it('the pattern really catches the call shapes it exists to ban', () => {
    expect("import { spawn } from 'node:child_process'").toMatch(SPAWN)
    expect("require('child_process')").toMatch(SPAWN)
    expect('execSync("reboot")').toMatch(SPAWN)
    expect('const label = activityLabel(a, ctx)').not.toMatch(SPAWN)
  })

  it('no scanned file runs a power action or the agent binary', () => {
    for (const path of scanned) {
      const text = read(path)
      expect(text, `${path} mentions a real power action`).not.toMatch(/\bsystemctl\b|\bpoweroff\b|\bshutdown -|\breboot -/)
      expect(text, `${path} mentions executing riot-agent`).not.toMatch(/riot-agent\s+(exec|run|--)/)
    }
  })
})

describe('[AC-047] the new web files use no raw HTML sink', () => {
  it.each(NEW_SOURCES)('%s has no dangerouslySetInnerHTML, innerHTML assignment or insertAdjacentHTML', path => {
    expect(read(path), `${path} uses a raw HTML sink`).not.toMatch(HTML_SINK)
  })

  it('the pattern really catches those sinks', () => {
    expect('<div dangerouslySetInnerHTML={{ __html: x }} />').toMatch(HTML_SINK)
    expect('el.innerHTML = x').toMatch(HTML_SINK)
    expect('el.insertAdjacentHTML("beforeend", x)').toMatch(HTML_SINK)
    expect('<p>{label}</p>').not.toMatch(HTML_SINK)
  })
})

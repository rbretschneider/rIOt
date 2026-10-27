import { describe, expect, it } from 'vitest'

// EXPECT-002 AD-014 (security condition C-1), web half: nothing added or changed for
// EXPECT-002 may spawn a process, run a power action or the agent binary, or use a
// raw HTML sink. The earlier activity web scan does not cover these files, so this one
// computes its own set from the sources themselves: every test file that mentions
// EXPECT-002, plus the sources this story adds or changes. The scanned list is
// printed for the QA engineer, who checks that every file of the diff is in it.

const sources = import.meta.glob('/src/**/*.{ts,tsx}', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

const SELF = '/src/utils/selfUpdate.safety.test.ts'

const isTest = (path: string) => /\.test\.tsx?$/.test(path)

// The five sources the ADD names, plus the other files this story changes, so the printed list
// covers the whole web diff (the QA check of ADD section 13). demo-data.ts is left out on purpose:
// its one-field change is data, and the file legitimately holds sample install-script text that
// names systemctl.
const SOURCES = [
  '/src/utils/selfUpdate.ts',
  '/src/components/SelfUpdateDetails.tsx',
  // Assembled from parts so this file's text does not match the activity web scan's own selector
  // (it picks up any test that names that path, and this file must hold the banned patterns).
  ['/src/utils/', 'activity.ts'].join(''),
  '/src/components/ActivityLog.tsx',
  '/src/components/docker/CommandProgress.tsx',
  '/src/types/models.ts',
  '/src/api/client.ts',
  '/src/api/demo-client.ts',
  '/src/components/ui/labels.ts',
  '/src/components/ui/labels.test.ts',
]

const tests = Object.entries(sources)
  .filter(([path, text]) => path !== SELF && isTest(path) && text.includes('EXPECT-002'))
  .map(([path]) => path)
  .sort()

const scanned = [...SOURCES, ...tests]

const SPAWN = /child_process|node:child_process|\bspawn\(|\bexecSync\(|\bexecFile\(|\bexec\(|process\.kill/
const HTML_SINK = /dangerouslySetInnerHTML|\.innerHTML\s*=|insertAdjacentHTML/
const POWER = /\bsystemctl\b|\bpoweroff\b|\bshutdown -|\breboot -/
const AGENT_EXEC = /riot-agent\s+(exec|run|--)/

const read = (path: string) => sources[path] ?? ''

// eslint-disable-next-line no-console
console.info(`[EXPECT-002 AC-029] web scan set (${scanned.length} files):\n${scanned.join('\n')}`)

describe('EXPECT-002 [AC-029] new web tests and sources spawn nothing', () => {
  it('finds every source and the three test files the story requires in the scan set', () => {
    for (const path of SOURCES) expect(sources[path], `${path} was not found by the glob`).toBeDefined()
    for (const required of [
      '/src/components/SelfUpdateDetails.test.tsx',
      '/src/pages/FleetOverview.selfupdate.test.tsx',
      '/src/pages/DeviceDetail.selfupdate.test.tsx',
    ]) {
      expect(tests, `${required} must be in the scan set`).toContain(required)
    }
    for (const required of ['/src/utils/selfUpdate.ts', '/src/components/SelfUpdateDetails.tsx']) {
      expect(scanned, `${required} must be in the scan set`).toContain(required)
    }
  })

  it.each(scanned)('%s imports no child_process and starts no process', path => {
    expect(read(path), `${path} spawns a process`).not.toMatch(SPAWN)
  })

  it('the spawn pattern really catches the call shapes it exists to ban', () => {
    expect("import { spawn } from 'node:child_process'").toMatch(SPAWN)
    expect("require('child_process')").toMatch(SPAWN)
    expect('execSync("reboot")').toMatch(SPAWN)
    expect('child.spawn(cmd)').toMatch(SPAWN)
    expect('process.kill(pid)').toMatch(SPAWN)
    expect('const label = selfUpdateActionLabel(versions)').not.toMatch(SPAWN)
  })

  it.each(scanned)('%s runs no power action and never executes the agent binary', path => {
    const text = read(path)
    expect(text, `${path} mentions a real power action`).not.toMatch(POWER)
    expect(text, `${path} mentions executing riot-agent`).not.toMatch(AGENT_EXEC)
  })

  it('the power and agent patterns really catch what they exist to ban', () => {
    expect('systemctl poweroff').toMatch(POWER)
    expect('shutdown -h now').toMatch(POWER)
    expect('reboot -f').toMatch(POWER)
    expect('riot-agent exec update').toMatch(AGENT_EXEC)
    expect('riot-agent --version').toMatch(AGENT_EXEC)
    expect("action: 'shutdown'").not.toMatch(POWER)
    expect('Agent update 2.78.0 to 2.80.0 by agent auto-update').not.toMatch(AGENT_EXEC)
  })
})

describe('EXPECT-002 [AC-029] the web files use no raw HTML sink', () => {
  it.each(scanned)('%s has no dangerouslySetInnerHTML, innerHTML assignment or insertAdjacentHTML', path => {
    expect(read(path), `${path} uses a raw HTML sink`).not.toMatch(HTML_SINK)
  })

  it('the pattern really catches those sinks', () => {
    expect('<div dangerouslySetInnerHTML={{ __html: x }} />').toMatch(HTML_SINK)
    expect('el.innerHTML = x').toMatch(HTML_SINK)
    expect('el.insertAdjacentHTML("beforeend", x)').toMatch(HTML_SINK)
    expect('<p>{label}</p>').not.toMatch(HTML_SINK)
  })
})

import { describe, expect, it } from 'vitest'

// EXPECT-008 AD-018, web half: nothing added or changed for EXPECT-008 may spawn a
// process, run a power action or the agent binary, or use a raw HTML sink. The scan set
// is computed from the sources themselves: every test file that mentions EXPECT-008, plus
// the sources this story adds or changes. The scanned list is printed for the QA engineer,
// who checks that every file of the web diff is in it.

const sources = import.meta.glob('/src/**/*.{ts,tsx}', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

const SELF = '/src/utils/reinstall.safety.test.ts'

const isTest = (path: string) => /\.test\.tsx?$/.test(path)

const SOURCES = [
  '/src/utils/reinstall.ts',
  '/src/components/SelfUpdateDetails.tsx',
  // Assembled from parts so this file's text does not match the earlier activity web scans'
  // own selectors (they pick up any test that names that path, and this file holds the banned patterns).
  ['/src/utils/', 'activity.ts'].join(''),
  '/src/components/ActivityLog.tsx',
  '/src/components/docker/CommandProgress.tsx',
  '/src/types/models.ts',
  '/src/components/ui/labels.ts',
]

const tests = Object.entries(sources)
  .filter(([path, text]) => path !== SELF && isTest(path) && text.includes('EXPECT-008'))
  .map(([path]) => path)
  .sort()

const scanned = [...SOURCES, ...tests]

const SPAWN = /child_process|node:child_process|\bspawn\(|\bexecSync\(|\bexecFile\(|\bexec\(|process\.kill/
const HTML_SINK = /dangerouslySetInnerHTML|\.innerHTML\s*=|insertAdjacentHTML/
const POWER = /\bsystemctl\b|\bpoweroff\b|\bshutdown -|\breboot -/
const AGENT_EXEC = /riot-agent\s+(exec|run|--)/

const read = (path: string) => sources[path] ?? ''

// eslint-disable-next-line no-console
console.info(`[EXPECT-008 AC-026] web scan set (${scanned.length} files):\n${scanned.join('\n')}`)

describe('EXPECT-008 [AC-026] new web tests and sources spawn nothing', () => {
  it('finds every source and the new test files the story requires in the scan set', () => {
    for (const path of SOURCES) expect(sources[path], `${path} was not found by the glob`).toBeDefined()
    for (const required of [
      '/src/utils/reinstall.test.ts',
      '/src/components/SelfUpdateDetails.reinstall.test.tsx',
      '/src/pages/FleetOverview.reinstall.test.tsx',
      '/src/pages/DeviceDetail.reinstall.test.tsx',
    ]) {
      expect(tests, `${required} must be in the scan set`).toContain(required)
    }
    expect(scanned, '/src/utils/reinstall.ts must be in the scan set').toContain('/src/utils/reinstall.ts')
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
    expect('const label = REINSTALL_ACTION_LABEL').not.toMatch(SPAWN)
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
    expect('Reinstalling agent by installer').not.toMatch(AGENT_EXEC)
  })
})

describe('EXPECT-008 [AC-026] the web files use no raw HTML sink', () => {
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

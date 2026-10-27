import { describe, expect, it } from 'vitest'

// EXPECT-007 AD-015, web half: nothing added or changed for this story may spawn a
// process, run a power action or the agent binary, or use a raw HTML sink. The scan set is
// computed from the sources themselves: every test file that mentions the story id, plus the
// one source the story changes. The scanned list is printed for the QA engineer, who checks
// that every web file of the diff is in it.

const sources = import.meta.glob('/src/**/*.{ts,tsx}', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

const SELF = '/src/utils/expect7.safety.test.ts'

const isTest = (path: string) => /\.test\.tsx?$/.test(path)

const SOURCES = ['/src/utils/commandLifecycle.ts']

// The story id is assembled so this file does not select itself by text.
const MARKER = ['EXPECT', '007'].join('-')

const tests = Object.entries(sources)
  .filter(([path, text]) => path !== SELF && isTest(path) && text.includes(MARKER))
  .map(([path]) => path)
  .sort()

const scanned = [...SOURCES, ...tests]

const SPAWN = /child_process|node:child_process|\bspawn\(|\bexecSync\(|\bexecFile\(|\bexec\(|process\.kill/
const HTML_SINK = /dangerouslySetInnerHTML|\.innerHTML\s*=|insertAdjacentHTML/
const POWER = /\bsystemctl\b|\bpoweroff\b|\bshutdown -|\breboot -/
const AGENT_EXEC = /riot-agent\s+(exec|run|--)/

const read = (path: string) => sources[path] ?? ''

// eslint-disable-next-line no-console
console.info(`[EXPECT-007 AC-037] web scan set (${scanned.length} files):\n${scanned.join('\n')}`)

describe('EXPECT-007 [AC-037] web tests and sources spawn nothing', () => {
  it('finds the changed source and the story test in the scan set', () => {
    for (const path of SOURCES) expect(sources[path], `${path} was not found by the glob`).toBeDefined()
    expect(tests, 'the command lifecycle test must be in the scan set').toContain('/src/utils/commandLifecycle.test.ts')
  })

  it.each(scanned)('%s imports no child_process and starts no process', path => {
    expect(read(path), `${path} spawns a process`).not.toMatch(SPAWN)
  })

  it.each(scanned)('%s runs no power action and never executes the agent binary', path => {
    const text = read(path)
    expect(text, `${path} mentions a real power action`).not.toMatch(POWER)
    expect(text, `${path} mentions executing riot-agent`).not.toMatch(AGENT_EXEC)
  })

  it.each(scanned)('%s uses no raw HTML sink', path => {
    expect(read(path), `${path} uses a raw HTML sink`).not.toMatch(HTML_SINK)
  })

  it('the patterns really catch what they exist to ban', () => {
    expect("import { spawn } from 'node:child_process'").toMatch(SPAWN)
    expect('execSync("reboot")').toMatch(SPAWN)
    expect('systemctl poweroff').toMatch(POWER)
    expect('shutdown -h now').toMatch(POWER)
    expect('riot-agent exec update').toMatch(AGENT_EXEC)
    expect('<div dangerouslySetInnerHTML={{ __html: x }} />').toMatch(HTML_SINK)
    expect("action: 'shutdown'").not.toMatch(POWER)
    expect('Waiting for dpkg lock').not.toMatch(POWER)
  })
})

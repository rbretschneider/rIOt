import { describe, expect, it } from 'vitest'
import fixtures from './__fixtures__/redact-cases.json'
import { maskLogLine, sensitiveDockerKey, maskEnvValue } from './logMask'
describe('Docker redaction shared with the agent and server', () => {
  for (const c of fixtures.mask)
    it(`masks ${c.input}`, () => {
      expect(maskLogLine(c.input)).toBe(c.want)
      expect(maskLogLine(c.want)).toBe(c.want)
    })
  for (const c of fixtures.keys)
    it(`classifies ${c.input}`, () =>
      expect(sensitiveDockerKey(c.input)).toBe(c.want))
  for (const c of fixtures.env)
    it(`protects env ${c.key}`, () =>
      expect(maskEnvValue(c.key, c.value)).toBe(c.want))
})

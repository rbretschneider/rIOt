/// <reference types="node" />
import { readFileSync } from 'node:fs'
import { describe, it, expect } from 'vitest'
import { TOKEN_HEX, TOKEN_PALETTE, Z_INDEX, type TokenName } from './tokens'

// vitest does not process CSS (an `index.css?raw` import resolves to an empty
// string), so the stylesheet is read from disk instead (vitest runs with web/ as the working directory).
const css = readFileSync('src/index.css', 'utf8')

const REQUIRED_TOKENS = [
  'surface',
  'raised',
  'border',
  'text',
  'text-muted',
  'accent',
  'success',
  'warn',
  'danger',
  'info',
] as const

/** Returns the body of the first block that starts with the given at-rule header. */
function blockBody(source: string, header: string): string {
  const start = source.indexOf(header)
  if (start === -1) return ''
  const open = source.indexOf('{', start)
  let depth = 0
  for (let i = open; i < source.length; i++) {
    if (source[i] === '{') depth++
    if (source[i] === '}') {
      depth--
      if (depth === 0) return source.slice(open + 1, i)
    }
  }
  return ''
}

function declaredVars(body: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const m of body.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) out[m[1]] = m[2].trim()
  return out
}

function luminance(hex: string): number {
  const channels = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255)
  const [r, g, b] = channels.map(c => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4))
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

describe('[UIF AC-001] color tokens defined in @theme', () => {
  const vars = declaredVars(blockBody(css, '@theme inline'))

  it.each(REQUIRED_TOKENS)('defines --color-%s inside @theme inline', token => {
    expect(vars[`--color-${token}`], `--color-${token} missing from @theme inline`).toBeDefined()
  })

  it('declares the tokens inside a Tailwind @theme block, not a plain :root rule', () => {
    expect(css).toMatch(/@theme inline\s*\{/)
  })
})

describe('[UIF AC-002] token values alias the existing palette', () => {
  const vars = declaredVars(blockBody(css, '@theme inline'))

  it.each(Object.entries(TOKEN_PALETTE))('--color-%s aliases palette step %s', (token, palette) => {
    expect(vars[`--color-${token}`]).toBe(`var(--color-${palette})`)
  })

  it('declares no token that tokens.ts does not know about', () => {
    const declared = Object.keys(vars).map(v => v.replace('--color-', ''))
    expect(declared.sort()).toEqual(Object.keys(TOKEN_PALETTE).sort())
  })

  it('keeps success on the emerald family and info on blue (documented DEV-01 deviation)', () => {
    expect(TOKEN_PALETTE.success).toBe('emerald-500')
    expect(TOKEN_PALETTE.info).toBe('blue-500')
  })
})

describe('[UIF AC-003] z-index layering order', () => {
  const rootVars = declaredVars(blockBody(css, ':root'))
  const read = (name: string) => Number(rootVars[`--z-${name}`])

  it('defines the five layers on :root', () => {
    expect(['base', 'dropdown', 'modal', 'tooltip', 'toast'].map(read)).toEqual([0, 40, 50, 70, 80])
  })

  it('orders dropdown < modal < tooltip < toast', () => {
    expect(read('dropdown')).toBeLessThan(read('modal'))
    expect(read('modal')).toBeLessThan(read('tooltip'))
    expect(read('tooltip')).toBeLessThan(read('toast'))
  })

  it('matches the Z_INDEX mirror used by tests', () => {
    expect(Z_INDEX).toEqual({
      base: read('base'),
      dropdown: read('dropdown'),
      modal: read('modal'),
      tooltip: read('tooltip'),
      toast: read('toast'),
    })
  })
})

describe('[UIF AC-007] text tokens meet WCAG AA 4.5:1', () => {
  const textTokens: TokenName[] = ['text', 'text-muted']
  const backgrounds: TokenName[] = ['surface', 'raised']

  for (const fg of textTokens) {
    for (const bg of backgrounds) {
      it(`${fg} on ${bg} is at least 4.5:1`, () => {
        const ratio = contrast(TOKEN_HEX[fg], TOKEN_HEX[bg])
        expect(ratio, `${fg} on ${bg} contrast was ${ratio.toFixed(2)}`).toBeGreaterThanOrEqual(4.5)
      })
    }
  }

  it('would reject gray-500 as muted text on raised (guards the A-013 choice)', () => {
    expect(contrast('#6b7280', TOKEN_HEX.raised)).toBeLessThan(4.5)
  })
})

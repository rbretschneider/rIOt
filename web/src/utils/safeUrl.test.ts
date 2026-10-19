import { expect, it } from 'vitest'
import { safeUrl, portUrl, parseIP } from './safeUrl'
it('only links safe web schemes without credentials', () => {
  expect(safeUrl('javascript:alert(1)')).toBeUndefined()
  expect(safeUrl('https://user:pass@host/')).toBeUndefined()
  expect(safeUrl('https://example.com')).toBe('https://example.com/')
})
it('only builds port links for strict literal addresses and valid port ranges', () => {
  expect(portUrl('192.168.1.2', '8080')).toBe('http://192.168.1.2:8080/')
  expect(portUrl('::1', '80')).toBeUndefined()
  for (const ip of [
    '999.1.2.3',
    'localhost',
    '01.1.2.3',
    'evil/path',
    '1.2.3.4@evil',
  ])
    expect(parseIP(ip)).toBeUndefined()
  expect(portUrl('1.2.3.4', '0')).toBeUndefined()
  expect(portUrl('1.2.3.4', '65536')).toBeUndefined()
  expect(portUrl('1.2.3.4', '80', 'javascript')).toBeUndefined()
})

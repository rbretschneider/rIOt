import { expect, it } from 'vitest'
import {
  parseRecentResources,
  resourceFromPath,
  resourcePath,
} from './recentResources'
it('stores canonical resource identities in the specified type/key shape', () => {
  expect(resourceFromPath('/docker/containers/host-1/web')).toEqual({
    type: 'container',
    key: 'host-1:web',
  })
  expect(resourceFromPath('/docker/stacks/host-1/my%20app')).toEqual({
    type: 'stack',
    key: 'host-1:my app',
  })
  expect(resourceFromPath('/devices/host-1')).toEqual({
    type: 'host',
    key: 'host-1',
  })
  expect(resourcePath({ type: 'stack', key: 'host-1:my app' })).toBe(
    '/docker/stacks/host-1/my%20app',
  )
})
it('reads legacy paths, drops malformed entries and deduplicates recents', () => {
  expect(
    parseRecentResources([
      '/devices/host-1',
      { type: 'host', key: 'host-1' },
      '/docker/containers/host-2/web',
      { type: 'command', key: 'danger' },
      { type: 'stack', key: 'invalid' },
      null,
    ]),
  ).toEqual([
    { type: 'host', key: 'host-1' },
    { type: 'container', key: 'host-2:web' },
  ])
  expect(parseRecentResources('bad')).toEqual([])
})
it('keeps at most eight recent resources', () => {
  expect(
    parseRecentResources(
      Array.from({ length: 20 }, (_, i) => ({
        type: 'host',
        key: `host-${i}`,
      })),
    ),
  ).toHaveLength(8)
})

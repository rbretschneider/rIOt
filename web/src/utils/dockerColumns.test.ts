import { beforeEach, describe, expect, it } from 'vitest'
import {
  ADDED_DOCKER_COLUMNS,
  DEFAULT_DOCKER_COLUMNS,
  loadDockerColumns,
  saveDockerColumns,
} from './dockerColumns'

beforeEach(() => {
  localStorage.removeItem('riot-docker-columns')
  localStorage.removeItem('riot-docker-columns-added')
})

describe('[AC-013] new columns appear once for existing users', () => {
  it('gives the defaults, which include Freshness and Auto-update, when nothing is saved', () => {
    expect(loadDockerColumns()).toEqual(DEFAULT_DOCKER_COLUMNS)
    expect(DEFAULT_DOCKER_COLUMNS).toEqual(expect.arrayContaining(['name', 'freshness', 'auto']))
    expect(JSON.parse(localStorage.getItem('riot-docker-columns-added')!)).toEqual(ADDED_DOCKER_COLUMNS)
  })
  it('appends the new columns to a set saved before they existed', () => {
    localStorage.setItem('riot-docker-columns', JSON.stringify(['name', 'stack']))
    expect(loadDockerColumns()).toEqual(['name', 'stack', 'freshness', 'auto'])
  })
  it('keeps a new column hidden after the user hides it', () => {
    localStorage.setItem('riot-docker-columns', JSON.stringify(['name', 'stack']))
    const shown = loadDockerColumns()
    saveDockerColumns(shown.filter((key) => key !== 'auto'))
    expect(loadDockerColumns()).toEqual(['name', 'stack', 'freshness'])
    expect(loadDockerColumns()).toEqual(['name', 'stack', 'freshness'])
  })
  it('always keeps Name, even when a saved set omits it', () => {
    localStorage.setItem('riot-docker-columns', JSON.stringify(['cpu']))
    localStorage.setItem('riot-docker-columns-added', JSON.stringify(ADDED_DOCKER_COLUMNS))
    expect(loadDockerColumns()).toEqual(['name', 'cpu'])
  })
  it('falls back to the defaults for an unreadable saved set', () => {
    localStorage.setItem('riot-docker-columns', '{broken')
    expect(loadDockerColumns()).toEqual(DEFAULT_DOCKER_COLUMNS)
  })
})

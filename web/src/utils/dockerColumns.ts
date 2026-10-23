// Visible-column preference of the Containers table. Columns added after a
// user first saved a set are shown to them once: the added marker records which
// new columns were already applied, so hiding one later sticks.
export const DEFAULT_DOCKER_COLUMNS = [
  'name',
  'host',
  'stack',
  'status',
  'freshness',
  'auto',
  'cpu',
  'mem',
  'links',
]
export const ADDED_DOCKER_COLUMNS = ['freshness', 'auto']
const columnsKey = 'riot-docker-columns'
const addedKey = 'riot-docker-columns-added'

const stringList = (value: unknown): value is string[] =>
  Array.isArray(value) && value.every((key) => typeof key === 'string')

function read(key: string): unknown {
  try {
    return JSON.parse(localStorage.getItem(key) ?? 'null')
  } catch {
    return null
  }
}

function write(key: string, value: string[]) {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    /* Session-only preference when browser storage is unavailable. */
  }
}

export function loadDockerColumns(): string[] {
  const saved = read(columnsKey)
  const applied = read(addedKey)
  if (!stringList(saved)) {
    write(addedKey, ADDED_DOCKER_COLUMNS)
    return DEFAULT_DOCKER_COLUMNS
  }
  const marker = stringList(applied) ? applied : []
  const visible = [
    ...new Set([
      'name',
      ...saved,
      ...ADDED_DOCKER_COLUMNS.filter((key) => !marker.includes(key)),
    ]),
  ]
  write(columnsKey, visible)
  write(addedKey, [...new Set([...marker, ...ADDED_DOCKER_COLUMNS])])
  return visible
}

export function saveDockerColumns(keys: string[]) {
  write(columnsKey, keys)
}

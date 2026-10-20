import { api, CommandConflictError } from '../api/client'
import type {
  DockerHost,
  DockerRow,
  DockerStack,
  LogHistory,
  ActivityResult,
  ActivityDetail,
  Inventory,
  PrunePreview,
  BulkResult,
} from '../types/docker'
import type { Command } from '../types/models'
import { dockerDemo } from './dockerDemo'
const enc = encodeURIComponent
const root = '/api/v1'
export class DockerRequestError extends Error {
  constructor(
    message: string,
    readonly code: string,
    readonly existingCommandId?: string,
  ) {
    super(message)
    this.name = 'DockerRequestError'
  }
}
async function request<T>(
  path: string,
  body?: unknown,
  read = false,
): Promise<T> {
  const response = await fetch(root + path, {
    credentials: 'same-origin',
    method: body === undefined ? 'GET' : 'POST',
    headers: {
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      ...(read ? { 'X-Riot-Request': '1' } : {}),
    },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  })
  if (response.status === 401) {
    window.location.reload()
    throw new Error('Session expired')
  }
  if (!response.ok) {
    const error = await response.json().catch(() => ({}))
    if (
      response.status === 409 &&
      error.code === 'conflict' &&
      typeof error.existing_command_id === 'string'
    )
      throw new CommandConflictError({ ...error, error: 'Already in progress' })
    throw new DockerRequestError(
      error.code === 'conflict'
        ? 'Already in progress'
        : error.error || error.code || `HTTP ${response.status}`,
      error.code ?? '',
      error.existing_command_id,
    )
  }
  return response.status === 204 ? (undefined as T) : response.json()
}
const resource = (id: string, kind: string, name: string) =>
  `/devices/${enc(id)}/docker/${kind}/${enc(name)}`
const real = {
  hosts: () => request<DockerHost[]>('/docker/hosts'),
  containers: () => api.getFleetContainers() as Promise<DockerRow[]>,
  bulk: (body: unknown) => request<BulkResult>('/docker/bulk-actions', body),
  history: (id: string, name: string, query: string, stack = false) =>
    request<LogHistory>(
      `${resource(id, stack ? 'stacks' : 'containers', name)}/logs?${query}`,
    ),
  inspect: (id: string, name: string) =>
    request<{ json: string; redactions: number; fetched_at: string }>(
      `${resource(id, 'containers', name)}/inspect`,
      undefined,
      true,
    ),
  stacks: () => request<DockerStack[]>('/docker/stacks'),
  stack: (id: string, project: string) =>
    request<DockerStack>(`/docker/stacks/${enc(id)}/${enc(project)}`),
  stackAction: (id: string, project: string, body: unknown) =>
    request<Command>(`${resource(id, 'stacks', project)}/actions`, body),
  forget: (id: string, project: string) =>
    request<void>(`${resource(id, 'stacks', project)}/forget`, {}),
  acknowledge: (id: string, project: string, body: unknown) =>
    request<{ path_state: string }>(
      `${resource(id, 'stacks', project)}/paths/acknowledge`,
      body,
    ),
  compose: (id: string, project: string) =>
    request<{
      working_dir: string
      files: { index: number; name: string; status: string }[]
      env: { present: boolean; status: string }
    }>(`${resource(id, 'stacks', project)}/compose`, undefined, true),
  composeFile: (id: string, project: string, index: number) =>
    request<{ name: string; content: string; redactions: number }>(
      `${resource(id, 'stacks', project)}/compose/files/${index}`,
      undefined,
      true,
    ),
  env: (id: string, project: string) =>
    request<{ keys: { key: string; value: string }[]; redactions: number }>(
      `${resource(id, 'stacks', project)}/env`,
      undefined,
      true,
    ),
  activity: (query = '') =>
    request<ActivityResult>(`/docker/activity?${query}`),
  activityDetail: (id: string, deviceId: string) =>
    request<ActivityDetail>(
      `/docker/activity/commands/${enc(id)}?device_id=${enc(deviceId)}`,
    ),
  inventory: (kind: string) =>
    request<Inventory>(`/docker/inventory?kind=${enc(kind)}`),
  refresh: (id: string) =>
    request<Command>(`/devices/${enc(id)}/docker/inventory/refresh`, {}),
  preview: (id: string, body: unknown) =>
    request<PrunePreview>(`/devices/${enc(id)}/docker/prune/preview`, body),
  execute: (id: string, body: unknown) =>
    request<Command>(`/devices/${enc(id)}/docker/prune/execute`, body),
}
export const dockerApi: typeof real =
  import.meta.env.VITE_DEMO === 'true' ? dockerDemo : real

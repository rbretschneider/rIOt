import { api } from '../api/client'
import type {
  DockerHost,
  DockerStack,
  Inventory,
  PrunePreview,
  LogHistory,
  DockerRow,
  ActivityResult,
  ActivityDetail,
} from '../types/docker'
const caps = [
  'docker_meta_v1',
  'bulk_results_v1',
  'container_locks_v1',
  'logs_live_v1',
  'inspect_v1',
  'cmd_steps_v1',
  'stack_actions_v1',
  'compose_read_v1',
  'inventory_v1',
  'prune_v1',
]
const now = () => new Date().toISOString()
const command = () => api.sendCommand('demo', 'docker_inventory_refresh', {})
const fleet = async (): Promise<DockerRow[]> => {
  const devices = await api.getDevices()
  const rows = await api.getFleetContainers()
  const containers = await api.getDeviceContainers(devices[0].id)
  const current = rows.map((row) => {
    const c = containers.find((c) => c.name === row.container_name)
    return {
      ...row,
      short_id: c?.short_id,
      status: c?.status,
      ports: c?.ports,
      started_at: c?.started_at,
      finished_at: c?.finished_at,
      exit_code: c?.exit_code,
      health_status: c?.health_status,
      riot_name: c?.riot?.name,
      update_status: c?.update_status,
      mem_limit_configured: true,
      target_key: `docker:container:${row.container_name}`,
      stack: c?.labels?.['com.docker.compose.project'] ?? row.stack,
    }
  })
  const legacy = current.slice(0, 2).map((row) => ({
    ...row,
    device_id: devices[1].id,
    hostname: devices[1].hostname,
    started_at: undefined,
    finished_at: undefined,
    exit_code: undefined,
    update_status: undefined,
  }))
  return [...current, ...legacy]
}
const stacks = async (): Promise<DockerStack[]> => {
  const rows = await fleet()
  const result = new Map<string, DockerStack>()
  for (const r of rows.filter((r) => r.stack)) {
    const key = r.device_id + r.stack
    const s = result.get(key) ?? {
      device_id: r.device_id,
      hostname: r.hostname,
      project: r.stack,
      state: 'running',
      running: 0,
      total: 0,
      updates: 0,
      auto_update: false,
      last_seen_at: now(),
      path_state: 'agreed',
      agreed_working_dir: '/srv/stacks/' + r.stack,
      agreed_config_files: ['compose.yaml'],
      services: [],
    }
    s.total++
    if (r.state === 'running') s.running++
    if (r.update_available) s.updates++
    s.services!.push(r)
    result.set(key, s)
  }
  for (const stack of result.values())
    stack.state =
      stack.running === 0
        ? 'down'
        : stack.running === stack.total
          ? 'running'
          : 'partial'
  const host = rows[0]
  return [
    ...result.values(),
    {
      device_id: host.device_id,
      hostname: host.hostname,
      project: 'nightly-backup',
      state: 'down',
      running: 0,
      total: 0,
      updates: 0,
      auto_update: false,
      last_seen_at: now(),
      path_state: 'agreed',
      agreed_working_dir: '/srv/stacks/nightly-backup',
      agreed_config_files: ['compose.yaml'],
      services: [],
    },
  ]
}
export const dockerDemo = {
  hosts: async (): Promise<DockerHost[]> =>
    (await api.getDevices()).map((d, index) => ({
      device_id: d.id,
      hostname: d.hostname,
      status: d.status,
      docker_available: index < 2,
      capabilities: index === 1 ? [] : caps,
      inventory_enabled: index === 0,
      primary_ip: index === 0 ? '192.168.10.20' : '192.168.10.30',
      container_logs: 'enabled',
    })),
  containers: fleet,
  bulk: async (body: unknown) => ({
    results: (
      body as { targets: { device_id: string; container: string }[] }
    ).targets.map((t) => ({
      ...t,
      outcome: 'created',
      command_id: 'demo-' + Math.random().toString(16).slice(2),
    })),
  }),
  history: async (
    _id: string,
    name: string,
    _query: string,
    _stack = false,
  ): Promise<LogHistory> => ({
    entries: Array.from({ length: 45 }, (_, i) => ({
      t: new Date(Date.now() - (45 - i) * 10000).toISOString(),
      s: i % 9 ? 'o' : 'e',
      l: `${name}: ${i % 9 ? 'Request completed successfully' : 'Retrying connection'}`,
    })),
    has_more: false,
    server_now: now(),
    retention_days: 7,
    oldest_retained_at: new Date(Date.now() - 604800000).toISOString(),
    range_clamped: false,
    pre_recreate_history: true,
    collector: 'enabled',
  }),
  inspect: async (_id: string, name: string) => ({
    json: JSON.stringify(
      {
        Name: name,
        Config: { Env: ['TOKEN=<redacted>'], Image: 'nginx:latest' },
      },
      null,
      2,
    ),
    redactions: 1,
    fetched_at: now(),
  }),
  stacks,
  stack: async (id: string, project: string) => {
    const stack = (await stacks()).find(
      (s) => s.device_id === id && s.project === project,
    )
    if (!stack) throw new Error('Stack not found')
    return stack
  },
  stackAction: async (_id: string, _project: string, _body: unknown) =>
    command(),
  forget: async (_id: string, _project: string) => undefined,
  acknowledge: async (_id: string, _project: string, _body: unknown) => ({
    path_state: 'agreed',
  }),
  compose: async (_id: string, project: string) => ({
    working_dir: '/srv/stacks/' + project,
    files: [{ index: 0, name: 'compose.yaml', status: 'ok' }],
    env: { present: true, status: 'ok' },
  }),
  composeFile: async (_id: string, _project: string, _index: number) => ({
    name: 'compose.yaml',
    content:
      'services:\n  web:\n    image: nginx:latest\n    environment:\n      TOKEN: <redacted>\n',
    redactions: 1,
  }),
  env: async (_id: string, _project: string) => ({
    keys: [{ key: 'TOKEN', value: '<redacted>' }],
    redactions: 1,
  }),
  activity: async (query = ''): Promise<ActivityResult> => {
    const rows = await fleet()
    const first = rows[0]
    const p = new URLSearchParams(query)
    const entries = [
      {
        kind: 'command',
        id: 'demo-update',
        device_id: first.device_id,
        hostname: first.hostname,
        container: 'plex',
        stack: 'media',
        action: 'docker_update',
        actor_kind: 'operator',
        actor_name: 'Operator',
        actor_sub: 'demo-operator',
        status: 'succeeded',
        message: 'Image updated and container verified healthy',
        requested_at: new Date(Date.now() - 1800000).toISOString(),
        duration_ms: 18300,
        target_exists: true,
      },
      {
        kind: 'command',
        id: 'demo-failure',
        device_id: first.device_id,
        hostname: first.hostname,
        container: 'homeassistant',
        stack: 'automation',
        action: 'docker_update',
        actor_kind: 'system',
        actor_name: 'System',
        actor_sub: '',
        status: 'failed',
        failure_step: 'pull',
        message: 'Registry unavailable; running container unchanged',
        requested_at: new Date(Date.now() - 3600000).toISOString(),
        duration_ms: 30000,
        target_exists: true,
      },
      {
        kind: 'event',
        id: 'demo-event',
        device_id: first.device_id,
        hostname: first.hostname,
        container: 'watchtower',
        stack: 'management',
        action: 'container_stopped',
        actor_kind: 'agent_observed',
        actor_name: 'Agent (observed)',
        actor_sub: '',
        message: 'Container exited with code 0',
        requested_at: new Date(Date.now() - 7200000).toISOString(),
        target_exists: true,
      },
    ]
    return {
      entries: entries.filter(
        (e) =>
          (!p.get('device_id') || p.get('device_id') === e.device_id) &&
          (!p.get('container') || p.get('container') === e.container) &&
          (!p.get('stack') || p.get('stack') === e.stack) &&
          (!p.get('type') || p.get('type') === e.kind) &&
          (!p.get('state') || p.get('state') === e.status),
      ),
      next_cursor: undefined,
    }
  },
  activityDetail: async (
    id: string,
    _deviceId: string,
  ): Promise<ActivityDetail> => {
    const entries = await dockerDemo.activity()
    const entry = entries.entries.find((e) => e.id === id) ?? entries.entries[0]
    return {
      entry,
      steps: [
        {
          seq: 1,
          name: 'pull',
          outcome: id === 'demo-failure' ? 'failed' : 'succeeded',
          output:
            id === 'demo-failure'
              ? 'Registry connection timed out'
              : 'Downloaded newer image\nRegistry token: <masked>',
        },
        {
          seq: 2,
          name: 'verify',
          outcome: 'succeeded',
          output: 'Health check passed',
        },
      ],
      output_truncated: false,
      target_results: [],
      events: [],
    }
  },
  inventory: async (kind: string): Promise<Inventory> => {
    const hosts = (await dockerDemo.hosts()).filter(
      (host) => host.docker_available,
    )
    return {
      hosts: hosts.map((h) => ({
        ...h,
        state: h.inventory_enabled ? 'ok' : 'unsupported',
        stale: false,
        truncated: false,
        collected_at: now(),
        disk_usage: { images: { total: 2600000000, reclaimable: 260000000 } },
      })),
      items: hosts
        .filter((host) => host.inventory_enabled)
        .flatMap((h) =>
          Array.from({ length: 3 }, (_, i) => ({
            device_id: h.device_id,
            id: `demo-${kind}-${i}`,
            name: `${kind}-${i}`,
            tags:
              kind === 'images' ? [`nginx:${i ? 'latest' : 'old'}`] : undefined,
            size: 87000000,
            containers:
              kind === 'networks' ? (i > 1 ? ['web'] : []) : i > 1 ? 1 : 0,
            used_by: i > 1 ? ['web'] : [],
            anonymous: kind === 'volumes' && i === 0,
            dangling: i === 0,
            driver: kind === 'volumes' ? 'local' : 'bridge',
            built_in: false,
          })),
        ),
    }
  },
  refresh: async (_id: string) => command(),
  preview: async (id: string, body: unknown): Promise<PrunePreview> => {
    const request = body as {
      kind: string
      item_kind?: string
      items?: string[]
    }
    const kind =
      request.item_kind ??
      (request.kind === 'volumes'
        ? 'volume'
        : request.kind === 'networks'
          ? 'network'
          : 'image')
    const inventory = await dockerDemo.inventory(`${kind}s`)
    const candidates = inventory.items.filter(
      (item) =>
        item.device_id === id &&
        !item.used_by?.length &&
        (request.kind !== 'images' || item.dangling) &&
        (request.kind !== 'selected' ||
          request.items?.includes(item.id ?? item.name!)),
    )
    const items = candidates.map((item) => ({
      id: item.id ?? item.name!,
      name: item.name ?? item.id!,
      tags: item.tags,
      size: item.size,
      selected_default: kind !== 'volume' || item.anonymous === true,
    }))
    return {
      preview_id: 'demo-preview',
      item_kind: kind,
      items,
      count: items.length,
      reclaimable_bytes: items.reduce(
        (total, item) => total + (item.size ?? 0),
        0,
      ),
      inventory_age_s: 0,
      requires_refresh: false,
      confirmation: { kind: kind === 'volume' ? 'host' : 'none' },
    }
  },
  execute: async (_id: string, _body: unknown) => command(),
}

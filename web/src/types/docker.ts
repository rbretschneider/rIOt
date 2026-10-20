import type { FleetContainerRow, Command, UpdateStatus } from './models'
export interface DockerHost {
  device_id: string
  hostname: string
  status: string
  primary_ip?: string
  docker_available: boolean
  capabilities: string[]
  inventory_enabled: boolean
  self_role?: string
  container_logs: string
  storage_quota?: { mode?: { logs?: string }; fairness?: { limited: boolean } }
}
export interface DockerRow extends FleetContainerRow {
  short_id?: string
  status?: string
  health_status?: string
  started_at?: string
  finished_at?: string
  exit_code?: number
  target_key?: string
  stack_key?: string
  riot_name?: string
  riot_icon?: string
  riot_group?: string
  riot_tags?: string[]
  self_role?: string
  network_parent?: string
  ports?: {
    container_port: string
    host_port?: string
    host_ip?: string
    protocol: string
  }[]
  update_status?: UpdateStatus
  mem_limit_configured?: boolean
}
export interface DockerStack {
  device_id: string
  hostname: string
  project: string
  state: string
  running: number
  total: number
  updates: number
  auto_update: boolean
  self_role?: string
  last_seen_at: string
  path_state: string
  agreed_working_dir: string
  agreed_config_files: string[]
  observed_working_dir?: string
  observed_config_files?: string[]
  conflict_members?: string[]
  services?: DockerRow[]
}
export interface LogLine {
  t: string
  agent_t?: string
  s?: string
  l?: string
  x?: boolean
  container?: string
  gap?: { count: number; lower_bound: boolean; reason: string }
}
export interface LogHistory {
  entries: LogLine[]
  has_more: boolean
  next_before?: string
  server_now?: string
  retention_days: number
  oldest_retained_at: string
  range_clamped: boolean
  retention_limited_by?: string
  newest_stored_at?: string
  pre_recreate_history: boolean
  collector: string
}
export interface ActivityEntry {
  kind: string
  id: string
  device_id: string
  hostname: string
  container?: string
  stack?: string
  action: string
  actor_kind: string
  actor_name: string
  actor_sub: string
  attribution?: string
  status?: string
  step?: string
  failure_step?: string
  message: string
  requested_at: string
  duration_ms?: number
  target_exists: boolean
}
export interface ActivityResult {
  entries: ActivityEntry[]
  next_cursor?: string
}
export interface ActivityDetail {
  entry: ActivityEntry
  steps: {
    seq: number
    name: string
    output?: string
    outcome?: string
    truncated?: boolean
  }[]
  output_truncated: boolean
  target_results: unknown[]
  events: { type: string; created_at: string; message: string }[]
}
export interface InventoryItem {
  builtin?: boolean
  in_use?: boolean
  created_at?: string
  device_id: string
  id?: string
  name?: string
  tags?: string[]
  size?: number
  created?: string
  containers?: number | string[]
  used_by?: string[]
  dangling?: boolean
  anonymous?: boolean
  built_in?: boolean
  driver?: string
  project?: string
  scope?: string
  internal?: boolean
  mountpoint?: string
  subnets?: { subnet: string; gateway?: string }[]
}
export interface InventoryHost {
  device_id: string
  hostname: string
  state: string
  collected_at?: string
  stale: boolean
  truncated: unknown
  disk_usage?: Record<string, { total: number; reclaimable: number }>
}
export interface Inventory {
  hosts: InventoryHost[]
  items: InventoryItem[]
}
export interface PrunePreview {
  preview_id: string
  item_kind: string
  items: {
    id: string
    name: string
    size?: number
    selected_default: boolean
    tags?: string[]
  }[]
  count: number
  reclaimable_bytes: number
  inventory_age_s: number
  requires_refresh: boolean
  confirmation: { kind: string; count?: number }
}
export interface BulkResult {
  results: {
    device_id: string
    container: string
    outcome: string
    command_id?: string
    existing_command_id?: string
    reason?: string
  }[]
}
export type DockerCommand = Command

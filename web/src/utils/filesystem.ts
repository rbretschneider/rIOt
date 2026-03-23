/**
 * Shared filesystem classification and formatting utilities.
 *
 * POOL_FS_TYPES is a fallback mirror of the authoritative Go list in
 * internal/models/telemetry.go (PoolFSTypes). When adding a new pool
 * filesystem type, both locations must be updated.
 *
 * Device-path-based detection (mdraid, LVM) is mirrored from
 * internal/models/telemetry.go (IsPoolFilesystem) for backward compatibility
 * with pre-POOL-002 agents that do not set is_pool for these device types.
 * All three locations (Go PoolFSTypes, Go IsPoolFilesystem, TS isPoolFilesystem)
 * must be kept in sync when detection rules change.
 */

import type { Filesystem } from '../types/models'

/** Fallback mirror of Go PoolFSTypes. Kept in sync manually. */
export const POOL_FS_TYPES: readonly string[] = [
  'bcachefs',
  'btrfs',
  'fuse.mergerfs',
  'fuse.shfs',
  'fuse.unionfs',
  'mergerfs',
  'shfs',
  'zfs',
]

/**
 * Returns true if the filesystem is a pool/union filesystem.
 *
 * Uses is_pool from the agent when present (updated agents, POOL-001+).
 * Falls back to checking fs_type against POOL_FS_TYPES and device path
 * prefixes for backward compatibility with agents that predate POOL-002
 * (agents that do not perform device-path-based detection).
 *
 * Fallback detection mirrors Go IsPoolFilesystem in internal/models/telemetry.go.
 */
export function isPoolFilesystem(fs: Filesystem): boolean {
  if (fs.is_pool !== undefined) {
    return fs.is_pool === true
  }

  // Fallback for old agents: filesystem-type check
  if (POOL_FS_TYPES.includes(fs.fs_type)) {
    return true
  }

  // Fallback for old agents: device-path-based detection (mirrors Go IsPoolFilesystem)
  const device = fs.device || ''

  // mdraid: /dev/md*
  if (device.startsWith('/dev/md')) {
    return true
  }

  // Device-mapper: /dev/mapper/*
  if (device.startsWith('/dev/mapper/')) {
    // Exclude Docker device-mapper volumes
    if (device.startsWith('/dev/mapper/docker-')) {
      return false
    }
    // Exclude live-boot overlay devices
    if (device === '/dev/mapper/live-rw' || device === '/dev/mapper/live-base') {
      return false
    }
    return true
  }

  // Device-mapper kernel name: /dev/dm-*
  if (device.startsWith('/dev/dm-')) {
    return true
  }

  return false
}

/**
 * Formats a capacity value in GB to a human-readable string.
 * Values >= 1000 GB are displayed as TB (divided by 1024, 2 decimal places).
 * Values < 1000 GB are displayed as GB (1 decimal place).
 */
export function formatCapacity(gb: number): string {
  if (gb >= 1000) {
    return `${(gb / 1024).toFixed(2)} TB`
  }
  return `${gb.toFixed(1)} GB`
}

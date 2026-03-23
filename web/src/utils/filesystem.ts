/**
 * Shared filesystem classification and formatting utilities.
 *
 * POOL_FS_TYPES is a fallback mirror of the authoritative Go list in
 * internal/models/telemetry.go (PoolFSTypes). When adding a new pool
 * filesystem type, both locations must be updated.
 */

import type { Filesystem } from '../types/models'

/** Fallback mirror of Go PoolFSTypes. Kept in sync manually. */
export const POOL_FS_TYPES: readonly string[] = [
  'bcachefs',
  'btrfs',
  'fuse.mergerfs',
  'fuse.unionfs',
  'mergerfs',
  'zfs',
]

/**
 * Returns true if the filesystem is a pool/union filesystem.
 *
 * Uses is_pool from the agent when present (updated agents).
 * Falls back to checking fs_type against POOL_FS_TYPES for backward
 * compatibility with agents that predate the is_pool field (NFR-002).
 */
export function isPoolFilesystem(fs: Filesystem): boolean {
  if (fs.is_pool !== undefined) {
    return fs.is_pool === true
  }
  return POOL_FS_TYPES.includes(fs.fs_type)
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

import { describe, it, expect } from 'vitest'
import { isPoolFilesystem, formatCapacity, POOL_FS_TYPES } from './filesystem'
import type { Filesystem } from '../types/models'

function makeFS(overrides: Partial<Filesystem>): Filesystem {
  return {
    mount_point: '/mnt/test',
    device: '/dev/sda1',
    fs_type: 'ext4',
    total_gb: 100,
    used_gb: 50,
    free_gb: 50,
    usage_percent: 50,
    is_network_mount: false,
    ...overrides,
  }
}

describe('[AC-001] mergerfs filesystem is classified as a pool', () => {
  it('returns true when is_pool is true (updated agent)', () => {
    const fs = makeFS({ fs_type: 'fuse.mergerfs', is_pool: true })
    expect(isPoolFilesystem(fs)).toBe(true)
  })

  it('returns true for fuse.mergerfs via fs_type fallback (old agent)', () => {
    const fs = makeFS({ fs_type: 'fuse.mergerfs' })
    expect(isPoolFilesystem(fs)).toBe(true)
  })
})

describe('[AC-002] Regular filesystem is not classified as a pool', () => {
  it('returns false for ext4 with no is_pool field', () => {
    const fs = makeFS({ fs_type: 'ext4' })
    expect(isPoolFilesystem(fs)).toBe(false)
  })

  it('returns false for tmpfs with no is_pool field', () => {
    const fs = makeFS({ fs_type: 'tmpfs' })
    expect(isPoolFilesystem(fs)).toBe(false)
  })

  it('returns false when is_pool is explicitly false (updated agent)', () => {
    const fs = makeFS({ fs_type: 'ext4', is_pool: false })
    expect(isPoolFilesystem(fs)).toBe(false)
  })
})

describe('[AC-007] Backward compatibility with old agents', () => {
  it('classifies fuse.mergerfs as pool when is_pool is absent', () => {
    const fs = makeFS({ fs_type: 'fuse.mergerfs' })
    // is_pool is not set — simulates old agent
    expect(fs.is_pool).toBeUndefined()
    expect(isPoolFilesystem(fs)).toBe(true)
  })

  it('classifies btrfs as pool when is_pool is absent', () => {
    const fs = makeFS({ fs_type: 'btrfs' })
    expect(fs.is_pool).toBeUndefined()
    expect(isPoolFilesystem(fs)).toBe(true)
  })

  it('classifies zfs as pool when is_pool is absent', () => {
    const fs = makeFS({ fs_type: 'zfs' })
    expect(fs.is_pool).toBeUndefined()
    expect(isPoolFilesystem(fs)).toBe(true)
  })

  it('classifies ext4 as non-pool when is_pool is absent', () => {
    const fs = makeFS({ fs_type: 'ext4' })
    expect(fs.is_pool).toBeUndefined()
    expect(isPoolFilesystem(fs)).toBe(false)
  })

  it('does not crash when is_pool is absent', () => {
    const fs = makeFS({ fs_type: 'vfat' })
    expect(() => isPoolFilesystem(fs)).not.toThrow()
  })
})

describe('[AC-008] ZFS and Btrfs filesystems are classified as pools', () => {
  it('classifies zfs as a pool via fs_type fallback', () => {
    const fs = makeFS({ fs_type: 'zfs' })
    expect(isPoolFilesystem(fs)).toBe(true)
  })

  it('classifies btrfs as a pool via fs_type fallback', () => {
    const fs = makeFS({ fs_type: 'btrfs' })
    expect(isPoolFilesystem(fs)).toBe(true)
  })

  it('classifies bcachefs as a pool via fs_type fallback', () => {
    const fs = makeFS({ fs_type: 'bcachefs' })
    expect(isPoolFilesystem(fs)).toBe(true)
  })

  it('classifies fuse.unionfs as a pool via fs_type fallback', () => {
    const fs = makeFS({ fs_type: 'fuse.unionfs' })
    expect(isPoolFilesystem(fs)).toBe(true)
  })

  it('classifies mergerfs (without fuse prefix) as a pool via fs_type fallback', () => {
    const fs = makeFS({ fs_type: 'mergerfs' })
    expect(isPoolFilesystem(fs)).toBe(true)
  })

  it('POOL_FS_TYPES covers all expected pool filesystem types', () => {
    const expected = ['bcachefs', 'btrfs', 'fuse.mergerfs', 'fuse.unionfs', 'mergerfs', 'zfs']
    for (const fsType of expected) {
      expect(POOL_FS_TYPES).toContain(fsType)
    }
  })
})

describe('[AC-006] Large capacity values display in TB', () => {
  it('formats 14000 GB as 13.67 TB', () => {
    expect(formatCapacity(14000)).toBe('13.67 TB')
  })

  it('formats 14400 GB as 14.06 TB', () => {
    expect(formatCapacity(14400)).toBe('14.06 TB')
  })

  it('formats exactly 1000 GB as TB (boundary at >= 1000)', () => {
    expect(formatCapacity(1000)).toBe('0.98 TB')
  })

  it('formats 999 GB as GB (boundary below 1000)', () => {
    expect(formatCapacity(999)).toBe('999.0 GB')
  })

  it('formats 500 GB as 500.0 GB', () => {
    expect(formatCapacity(500)).toBe('500.0 GB')
  })

  it('formats values >= 1000 GB as TB to 2 decimal places', () => {
    // 2048 GB = 2.00 TB (2048/1024)
    expect(formatCapacity(2048)).toBe('2.00 TB')
  })

  it('formats values < 1000 GB as GB to 1 decimal place', () => {
    expect(formatCapacity(100.5)).toBe('100.5 GB')
  })

  it('used and free capacity also use TB when >= 1000 GB', () => {
    // Verify formatCapacity is consistent regardless of which value it is called on
    expect(formatCapacity(8640)).toBe('8.44 TB')
    expect(formatCapacity(5760)).toBe('5.63 TB')
  })
})

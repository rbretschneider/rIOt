package models

import "testing"

// TestIsPoolFilesystem covers all acceptance criteria for pool detection.

// AC-001: Unraid shfs detection
func TestIsPoolFilesystem_AC001_ShfsIsPool(t *testing.T) {
	t.Run("[AC-001] shfs filesystem with arbitrary device is classified as a pool", func(t *testing.T) {
		if !IsPoolFilesystem("shfs", "/dev/shm") {
			t.Error("expected IsPoolFilesystem(\"shfs\", \"/dev/shm\") to return true")
		}
	})

	t.Run("[AC-001] shfs filesystem with empty device is classified as a pool", func(t *testing.T) {
		if !IsPoolFilesystem("shfs", "") {
			t.Error("expected IsPoolFilesystem(\"shfs\", \"\") to return true")
		}
	})
}

// AC-002: Unraid fuse.shfs detection
func TestIsPoolFilesystem_AC002_FuseShfsIsPool(t *testing.T) {
	t.Run("[AC-002] fuse.shfs filesystem with arbitrary device is classified as a pool", func(t *testing.T) {
		if !IsPoolFilesystem("fuse.shfs", "/dev/sda") {
			t.Error("expected IsPoolFilesystem(\"fuse.shfs\", \"/dev/sda\") to return true")
		}
	})
}

// AC-003: mdraid detection with ext4
func TestIsPoolFilesystem_AC003_MdraidExt4IsPool(t *testing.T) {
	t.Run("[AC-003] ext4 on /dev/md0 is classified as a pool", func(t *testing.T) {
		if !IsPoolFilesystem("ext4", "/dev/md0") {
			t.Error("expected IsPoolFilesystem(\"ext4\", \"/dev/md0\") to return true")
		}
	})
}

// AC-004: mdraid detection with multi-digit device number
func TestIsPoolFilesystem_AC004_MdraidMultiDigitIsPool(t *testing.T) {
	t.Run("[AC-004] xfs on /dev/md127 is classified as a pool", func(t *testing.T) {
		if !IsPoolFilesystem("xfs", "/dev/md127") {
			t.Error("expected IsPoolFilesystem(\"xfs\", \"/dev/md127\") to return true")
		}
	})

	t.Run("[AC-004] ext4 on /dev/md1 is classified as a pool", func(t *testing.T) {
		if !IsPoolFilesystem("ext4", "/dev/md1") {
			t.Error("expected IsPoolFilesystem(\"ext4\", \"/dev/md1\") to return true")
		}
	})
}

// AC-005: LVM mapper detection
func TestIsPoolFilesystem_AC005_LvmMapperIsPool(t *testing.T) {
	t.Run("[AC-005] ext4 on /dev/mapper/vg0-data is classified as a pool", func(t *testing.T) {
		if !IsPoolFilesystem("ext4", "/dev/mapper/vg0-data") {
			t.Error("expected IsPoolFilesystem(\"ext4\", \"/dev/mapper/vg0-data\") to return true")
		}
	})

	t.Run("[AC-005] xfs on /dev/mapper/data-lv is classified as a pool", func(t *testing.T) {
		if !IsPoolFilesystem("xfs", "/dev/mapper/data-lv") {
			t.Error("expected IsPoolFilesystem(\"xfs\", \"/dev/mapper/data-lv\") to return true")
		}
	})
}

// AC-006: LVM dm- kernel name detection
func TestIsPoolFilesystem_AC006_LvmDmIsPool(t *testing.T) {
	t.Run("[AC-006] xfs on /dev/dm-3 is classified as a pool", func(t *testing.T) {
		if !IsPoolFilesystem("xfs", "/dev/dm-3") {
			t.Error("expected IsPoolFilesystem(\"xfs\", \"/dev/dm-3\") to return true")
		}
	})

	t.Run("[AC-006] ext4 on /dev/dm-0 is classified as a pool", func(t *testing.T) {
		if !IsPoolFilesystem("ext4", "/dev/dm-0") {
			t.Error("expected IsPoolFilesystem(\"ext4\", \"/dev/dm-0\") to return true")
		}
	})
}

// AC-007: Docker device-mapper exclusion
func TestIsPoolFilesystem_AC007_DockerMapperExcluded(t *testing.T) {
	t.Run("[AC-007] /dev/mapper/docker-* device is not classified as a pool", func(t *testing.T) {
		if IsPoolFilesystem("ext4", "/dev/mapper/docker-253:0-1234-abcdef") {
			t.Error("expected IsPoolFilesystem(\"ext4\", \"/dev/mapper/docker-253:0-1234-abcdef\") to return false")
		}
	})

	t.Run("[AC-007] /dev/mapper/docker- prefix is excluded regardless of suffix", func(t *testing.T) {
		if IsPoolFilesystem("ext4", "/dev/mapper/docker-anything") {
			t.Error("expected /dev/mapper/docker-anything to return false")
		}
	})
}

// AC-008: Live-boot overlay exclusion
func TestIsPoolFilesystem_AC008_LiveBootExcluded(t *testing.T) {
	t.Run("[AC-008] /dev/mapper/live-rw is not classified as a pool", func(t *testing.T) {
		if IsPoolFilesystem("ext4", "/dev/mapper/live-rw") {
			t.Error("expected IsPoolFilesystem(\"ext4\", \"/dev/mapper/live-rw\") to return false")
		}
	})

	t.Run("[AC-008] /dev/mapper/live-base is not classified as a pool", func(t *testing.T) {
		if IsPoolFilesystem("ext4", "/dev/mapper/live-base") {
			t.Error("expected IsPoolFilesystem(\"ext4\", \"/dev/mapper/live-base\") to return false")
		}
	})
}

// AC-009: Regular ext4 not classified as pool
func TestIsPoolFilesystem_AC009_RegularExt4NotPool(t *testing.T) {
	t.Run("[AC-009] ext4 on /dev/sda1 is not classified as a pool", func(t *testing.T) {
		if IsPoolFilesystem("ext4", "/dev/sda1") {
			t.Error("expected IsPoolFilesystem(\"ext4\", \"/dev/sda1\") to return false")
		}
	})

	t.Run("[AC-009] regular filesystem types with standard devices are not pools", func(t *testing.T) {
		cases := []struct {
			fsType string
			device string
		}{
			{"ext4", "/dev/sda1"},
			{"xfs", "/dev/sdb2"},
			{"vfat", "/dev/sdc1"},
			{"tmpfs", "tmpfs"},
			{"", ""},
		}
		for _, c := range cases {
			if IsPoolFilesystem(c.fsType, c.device) {
				t.Errorf("expected IsPoolFilesystem(%q, %q) to return false", c.fsType, c.device)
			}
		}
	})
}

// AC-010: Existing pool types still detected
func TestIsPoolFilesystem_AC010_ExistingPoolTypesDetected(t *testing.T) {
	t.Run("[AC-010] zfs is still classified as a pool", func(t *testing.T) {
		if !IsPoolFilesystem("zfs", "tank/data") {
			t.Error("expected IsPoolFilesystem(\"zfs\", \"tank/data\") to return true")
		}
	})

	t.Run("[AC-010] All pre-existing pool filesystem types still return true", func(t *testing.T) {
		poolTypes := []string{
			"bcachefs",
			"btrfs",
			"fuse.mergerfs",
			"fuse.unionfs",
			"mergerfs",
			"zfs",
		}
		for _, fsType := range poolTypes {
			if !IsPoolFilesystem(fsType, "/dev/sda1") {
				t.Errorf("expected IsPoolFilesystem(%q, \"/dev/sda1\") to return true (regression)", fsType)
			}
		}
	})
}

// Additional edge cases for device-path detection

func TestIsPoolFilesystem_EmptyDeviceWithNonPoolFsType(t *testing.T) {
	t.Run("empty device string with non-pool fs_type returns false", func(t *testing.T) {
		if IsPoolFilesystem("ext4", "") {
			t.Error("expected IsPoolFilesystem(\"ext4\", \"\") to return false")
		}
	})
}

func TestIsPoolFilesystem_MdraidDetectionVariants(t *testing.T) {
	t.Run("mdraid: /dev/md with no suffix returns true (prefix match)", func(t *testing.T) {
		// /dev/md without number is not a real device but the prefix match is intentional per ADD.
		if !IsPoolFilesystem("ext4", "/dev/md") {
			t.Error("expected IsPoolFilesystem(\"ext4\", \"/dev/md\") to return true")
		}
	})
}

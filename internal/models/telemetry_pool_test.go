package models

import "testing"

func TestIsPoolFSType(t *testing.T) {
	t.Run("[AC-001] mergerfs filesystem is classified as a pool", func(t *testing.T) {
		if !IsPoolFSType("fuse.mergerfs") {
			t.Error("expected IsPoolFSType(\"fuse.mergerfs\") to return true")
		}
	})

	t.Run("[AC-008] ZFS filesystem is classified as a pool", func(t *testing.T) {
		if !IsPoolFSType("zfs") {
			t.Error("expected IsPoolFSType(\"zfs\") to return true")
		}
	})

	t.Run("[AC-008] Btrfs filesystem is classified as a pool", func(t *testing.T) {
		if !IsPoolFSType("btrfs") {
			t.Error("expected IsPoolFSType(\"btrfs\") to return true")
		}
	})

	t.Run("[AC-001] All pool filesystem types return true", func(t *testing.T) {
		poolTypes := []string{
			"bcachefs",
			"btrfs",
			"fuse.mergerfs",
			"fuse.unionfs",
			"mergerfs",
			"zfs",
		}
		for _, fsType := range poolTypes {
			if !IsPoolFSType(fsType) {
				t.Errorf("expected IsPoolFSType(%q) to return true", fsType)
			}
		}
	})

	t.Run("[AC-002] Regular filesystem ext4 is not classified as a pool", func(t *testing.T) {
		if IsPoolFSType("ext4") {
			t.Error("expected IsPoolFSType(\"ext4\") to return false")
		}
	})

	t.Run("[AC-002] Regular filesystem types are not classified as pools", func(t *testing.T) {
		nonPoolTypes := []string{
			"ext4",
			"tmpfs",
			"vfat",
			"xfs",
			"",
		}
		for _, fsType := range nonPoolTypes {
			if IsPoolFSType(fsType) {
				t.Errorf("expected IsPoolFSType(%q) to return false", fsType)
			}
		}
	})

	t.Run("[AC-002] Empty string is not classified as a pool", func(t *testing.T) {
		if IsPoolFSType("") {
			t.Error("expected IsPoolFSType(\"\") to return false")
		}
	})
}

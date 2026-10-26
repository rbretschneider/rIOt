package maintenance

import (
	"strings"
	"testing"
)

// A trimmed mountinfo of a Raspberry Pi 4 DietPi host before the 9 to 10
// firmware migration: ext4 root, FAT /boot, tmpfs /tmp, all shared.
const piMountinfo = `22 1 179:2 / / rw,noatime shared:1 - ext4 /dev/mmcblk0p2 rw,errors=remount-ro
23 22 179:1 / /boot rw,noatime shared:7 - vfat /dev/mmcblk0p1 rw,fmask=0022,dmask=0022
24 22 0:21 / /tmp rw,nosuid,nodev,noatime shared:3 - tmpfs tmpfs rw,size=1922840k
25 22 0:22 / /run rw,nosuid,nodev shared:5 - tmpfs tmpfs rw,size=768k,mode=755
`

func TestParseMountinfo_EU_OPS001_StrictAndBounded(t *testing.T) {
	t.Run("[OPS-001] a real-shaped table parses with its peer groups", func(t *testing.T) {
		entries, err := ParseMountinfo([]byte(piMountinfo))
		if err != nil || len(entries) != 4 {
			t.Fatalf("%v %v", entries, err)
		}
		boot, ok := topEntry(entries, "/boot")
		if !ok || boot.Shared != 7 || boot.FSType != "vfat" || boot.Source != "/dev/mmcblk0p1" {
			t.Errorf("/boot = %+v", boot)
		}
		tmp, _ := topEntry(entries, "/tmp")
		if !tmp.HasOption("nosuid") || !tmp.HasOption("nodev") || tmp.HasOption("noexec") || tmp.Shared != 3 {
			t.Errorf("/tmp = %+v", tmp)
		}
	})
	t.Run("[OPS-001] master and unbindable fields are understood", func(t *testing.T) {
		entries, err := ParseMountinfo([]byte("36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 shared:2 unbindable - ext3 /dev/root rw\n"))
		if err != nil || entries[0].Master != 1 || entries[0].Shared != 2 {
			t.Fatalf("%+v %v", entries, err)
		}
		if entries[0].Root != "/mnt1" {
			t.Errorf("root = %q", entries[0].Root)
		}
	})
	t.Run("[OPS-001] a mount with no optional fields has neither shared nor master", func(t *testing.T) {
		entries, err := ParseMountinfo([]byte("36 35 98:0 / /x rw - ext4 /dev/x rw\n"))
		if err != nil || entries[0].Shared != 0 || entries[0].Master != 0 {
			t.Fatalf("%+v %v", entries, err)
		}
	})
	t.Run("[OPS-001] octal escapes decode in paths", func(t *testing.T) {
		entries, err := ParseMountinfo([]byte(`36 35 98:0 / /mnt/with\040space\011tab\134slash rw shared:9 - ext4 /dev/x rw` + "\n"))
		if err != nil || entries[0].MountPoint != "/mnt/with space\ttab\\slash" {
			t.Fatalf("%q %v", entries[0].MountPoint, err)
		}
	})
	bad := map[string]string{
		"empty":                 "",
		"too few fields":        "36 35 98:0 / /x rw\n",
		"no terminator":         "36 35 98:0 / /x rw shared:1 ext4 /dev/x rw\n",
		"nothing after dash":    "36 35 98:0 / /x rw shared:1 - ext4\n",
		"bad id":                "x 35 98:0 / /x rw shared:1 - ext4 /dev/x rw\n",
		"bad parent":            "36 y 98:0 / /x rw shared:1 - ext4 /dev/x rw\n",
		"bad device":            "36 35 980 / /x rw shared:1 - ext4 /dev/x rw\n",
		"bad peer group":        "36 35 98:0 / /x rw shared:abc - ext4 /dev/x rw\n",
		"zero peer group":       "36 35 98:0 / /x rw shared:0 - ext4 /dev/x rw\n",
		"truncated escape":      `36 35 98:0 / /x\04 rw shared:1 - ext4 /dev/x rw` + "\n",
		"non-octal escape":      `36 35 98:0 / /x\0zz rw shared:1 - ext4 /dev/x rw` + "\n",
		"one bad line of three": piMountinfo + "garbage\n",
	}
	for name, text := range bad {
		t.Run("[OPS-001] rejects "+name, func(t *testing.T) {
			if _, err := ParseMountinfo([]byte(text)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	t.Run("[OPS-001] more than 8192 lines is refused", func(t *testing.T) {
		line := "36 35 98:0 / /x rw shared:1 - ext4 /dev/x rw\n"
		if _, err := ParseMountinfo([]byte(strings.Repeat(line, 8192))); err != nil {
			t.Fatalf("8192 lines must parse: %v", err)
		}
		if _, err := ParseMountinfo([]byte(strings.Repeat(line, 8193))); err == nil {
			t.Fatal("8193 lines accepted")
		}
	})
	t.Run("[OPS-001] more than 1 MiB is refused", func(t *testing.T) {
		if _, err := ParseMountinfo([]byte(piMountinfo + strings.Repeat("#", 1<<20))); err == nil {
			t.Fatal("an oversized table was accepted")
		}
	})
	t.Run("[OPS-001] topEntry returns the last of stacked mounts, containing returns the longest prefix", func(t *testing.T) {
		entries, _ := ParseMountinfo([]byte(piMountinfo + "40 22 0:30 / /tmp rw shared:99 - tmpfs tmpfs rw\n"))
		top, _ := topEntry(entries, "/tmp")
		if top.Shared != 99 {
			t.Errorf("top /tmp = %+v", top)
		}
		holder, _ := containing(entries, "/boot")
		if holder.MountPoint != "/boot" {
			t.Errorf("containing /boot = %q", holder.MountPoint)
		}
		holder, _ = containing(entries, "/boot/firmware")
		if holder.MountPoint != "/boot" {
			t.Errorf("containing /boot/firmware = %q, want /boot", holder.MountPoint)
		}
		holder, _ = containing(entries, "/bootleg")
		if holder.MountPoint != "/" {
			t.Errorf("/bootleg must not match /boot, got %q", holder.MountPoint)
		}
	})
}

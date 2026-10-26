package extraupd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func TestRegistry_EU_AC019_OnlyDietPiAndIDsMatchThePattern(t *testing.T) {
	reg := Registry()
	if len(reg) != 1 || reg[0].ID() != "dietpi" {
		t.Fatalf("registry = %v, want exactly dietpi", reg)
	}
	for _, u := range reg {
		if !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`).MatchString(u.ID()) {
			t.Errorf("id %q does not match the identifier pattern (FR-002)", u.ID())
		}
	}
	if _, ok := Lookup("snap"); ok {
		t.Error("snap must not be implemented in this story")
	}
	if !IsImplemented("dietpi") || IsImplemented("") || IsImplemented("DietPi") {
		t.Error("IsImplemented must be exact")
	}
	t.Run("a caller cannot reorder or swap the registry", func(t *testing.T) {
		r := Registry()
		r[0] = nil
		if Registry()[0] == nil {
			t.Error("Registry must return a copy")
		}
	})
}

func TestDetectAll_EU_AC001_IsBoundedAndOmitsAbsent(t *testing.T) {
	if got := DetectAll(context.Background(), newHost(t).fs()); got != nil {
		t.Errorf("DetectAll on a plain host = %+v, want nil", got)
	}
	got := DetectAll(context.Background(), dietpiHost(t, "10", "8", "2").fs())
	if len(got) != 1 || got[0].Readiness != "" {
		t.Errorf("DetectAll = %+v, want one entry with readiness left for the caller", got)
	}
}

func TestDirFS_EU_AC009_ReadsAreBoundedAndRegularOnly(t *testing.T) {
	h := newHost(t).write("a/file", "12345").mkdir("a/dir")
	fsys := h.fs()
	t.Run("[AC-009] a file within the bound is returned whole", func(t *testing.T) {
		data, err := fsys.ReadFile("a/file", 5)
		if err != nil || string(data) != "12345" {
			t.Errorf("got %q, %v", data, err)
		}
	})
	t.Run("[AC-009] a file one byte over the bound fails with ErrTooLarge", func(t *testing.T) {
		if _, err := fsys.ReadFile("a/file", 4); !errors.Is(err, ErrTooLarge) {
			t.Errorf("err = %v, want ErrTooLarge", err)
		}
	})
	t.Run("[AC-009] a directory is not a regular file", func(t *testing.T) {
		if _, err := fsys.ReadFile("a/dir", 10); !errors.Is(err, ErrNotRegular) {
			t.Errorf("err = %v, want ErrNotRegular", err)
		}
	})
	t.Run("[AC-009] a symlink is refused", func(t *testing.T) {
		if err := os.Symlink(filepath.Join(h.root, "a", "file"), filepath.Join(h.root, "a", "link")); err != nil {
			t.Skipf("symlinks unavailable here: %v", err)
		}
		if _, err := fsys.ReadFile("a/link", 10); !errors.Is(err, ErrNotRegular) {
			t.Errorf("err = %v, want ErrNotRegular", err)
		}
	})
	t.Run("[AC-009] names that escape the root are refused", func(t *testing.T) {
		for _, name := range []string{"", "/etc/passwd", "../x", "a/../../x", "a\x00b"} {
			if _, err := fsys.ReadFile(name, 10); err == nil {
				t.Errorf("ReadFile(%q) must fail", name)
			}
			if _, err := fsys.Lstat(name); err == nil {
				t.Errorf("Lstat(%q) must fail", name)
			}
		}
	})
}

func TestFixedEnv_EU_AC019_IsASeparateCopy(t *testing.T) {
	a := FixedEnv("X=1")
	b := FixedEnv()
	if len(a) != len(b)+1 || a[len(a)-1] != "X=1" {
		t.Errorf("FixedEnv(extra) = %v, base %v", a, b)
	}
	if !strings.HasPrefix(b[0], "PATH=") {
		t.Errorf("first entry = %q, want PATH", b[0])
	}
}

func TestModels_EU_AC035_ExtraUpdaterConstantsMatchTheRegistry(t *testing.T) {
	if DietPiID != models.ExtraUpdaterDietPi {
		t.Errorf("DietPiID %q != models.ExtraUpdaterDietPi %q", DietPiID, models.ExtraUpdaterDietPi)
	}
}

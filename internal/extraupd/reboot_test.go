package extraupd

import (
	"strings"
	"testing"
)

func TestModulesMissing_EU_AC010_RuleOnlyOnDietPi(t *testing.T) {
	t.Run("[AC-010] missing modules on a DietPi host mean reboot required", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2")
		h.write("proc/sys/kernel/osrelease", "6.12.99+rpt-rpi-v8\n")
		missing, release := ModulesMissing(h.fs())
		if !missing || release != "6.12.99+rpt-rpi-v8" {
			t.Fatalf("missing=%v release=%q, want true / 6.12.99+rpt-rpi-v8", missing, release)
		}
		required, reasons := RebootIndicators(h.fs())
		if !required || len(reasons) != 1 || !strings.Contains(reasons[0], "kernel modules for running kernel 6.12.99+rpt-rpi-v8 are missing") {
			t.Errorf("required=%v reasons=%v", required, reasons)
		}
	})
	t.Run("[AC-010] present modules leave the marker rule unchanged", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2")
		if required, reasons := RebootIndicators(h.fs()); required || len(reasons) != 0 {
			t.Errorf("required=%v reasons=%v, want nothing", required, reasons)
		}
		h.write("run/reboot-required", "")
		if required, reasons := RebootIndicators(h.fs()); !required || len(reasons) != 1 {
			t.Errorf("required=%v reasons=%v, want only the marker", required, reasons)
		}
	})
	t.Run("[AC-010] a non-DietPi host never reports missing modules", func(t *testing.T) {
		h := newHost(t).write("proc/sys/kernel/osrelease", "6.1.0-13-amd64\n")
		if missing, _ := ModulesMissing(h.fs()); missing {
			t.Error("the modules rule applies to DietPi hosts only (FR-016)")
		}
	})
	t.Run("[AC-010] an unreadable or malformed release adds nothing", func(t *testing.T) {
		for name, body := range map[string]string{"empty": "", "spaces": "6.1 0\n", "traversal": "../../etc\n", "too long": strings.Repeat("a", 65) + "\n", "newline junk": "6.1.0\nX\n"} {
			h := dietpiHost(t, "10", "8", "2").write("proc/sys/kernel/osrelease", body)
			if missing, _ := ModulesMissing(h.fs()); missing {
				t.Errorf("%s: release %q must not produce a claim", name, body)
			}
		}
		h := dietpiHost(t, "10", "8", "2")
		if err := removeFile(h.root, "proc/sys/kernel/osrelease"); err != nil {
			t.Fatal(err)
		}
		if missing, _ := ModulesMissing(h.fs()); missing {
			t.Error("a missing osrelease must not produce a claim")
		}
	})
}

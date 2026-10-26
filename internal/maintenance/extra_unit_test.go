package maintenance

import (
	"strings"
	"testing"
	"time"
)

func TestParseSystemdTimespan_EU_SEC002(t *testing.T) {
	ok := map[string]time.Duration{
		"35min":        35 * time.Minute,
		"1h 25min":     85 * time.Minute,
		"2100000000":   35 * time.Minute,
		"5100000000":   85 * time.Minute,
		"1min 30s":     90 * time.Second,
		"2h":           2 * time.Hour,
		"1d":           24 * time.Hour,
		"500ms":        500 * time.Millisecond,
		"1h 25min 30s": 85*time.Minute + 30*time.Second,
		" 1h  25min ":  85 * time.Minute,
	}
	for in, want := range ok {
		got, err := parseSystemdTimespan(in)
		if err != nil || got != want {
			t.Errorf("[SEC-002] parse %q = %s, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"", "infinity", "1h 25", "25 min", "min", "-5min", "1.5h", "1h 1h", "1week", "99999999999999999999", "5 minutes", "1h;rm", "1h\n25min x", "0x10", "1h25min"} {
		if got, err := parseSystemdTimespan(in); err == nil {
			t.Errorf("[SEC-002] parse %q = %s, want an error so an unreadable backstop is never trusted", in, got)
		}
	}
	t.Run("[SEC-002] the backstop the installer writes parses to at least the minimum", func(t *testing.T) {
		for _, rendering := range []string{"1h 25min", "5100000000"} {
			d, err := parseSystemdTimespan(rendering)
			if err != nil || d < ExtraMinBackstop || d != ExtraServiceBackstop {
				t.Errorf("%q = %s, %v; want %s", rendering, d, err, ExtraServiceBackstop)
			}
		}
	})
	t.Run("[SEC-002] the default 35 minute backstop is below the minimum", func(t *testing.T) {
		d, _ := parseSystemdTimespan("35min")
		if d >= ExtraMinBackstop {
			t.Errorf("35min = %s must be refused", d)
		}
	})
}

func TestParseUnitShow_EU_OPS001_ClosedGrammar(t *testing.T) {
	t.Run("[OPS-001] the three properties in any order parse", func(t *testing.T) {
		st, err := parseUnitShow("PrivateTmp=no\nMountFlags=\nRuntimeMaxUSec=1h 25min\n")
		if err != nil || st.RuntimeMax != 85*time.Minute || st.PrivateTmp != "no" || st.MountFlags != "" {
			t.Fatalf("%+v %v", st, err)
		}
	})
	t.Run("[OPS-001] sandbox values are carried through for the worker to refuse", func(t *testing.T) {
		st, err := parseUnitShow("RuntimeMaxUSec=5100000000\nPrivateTmp=yes\nMountFlags=slave\n")
		if err != nil || st.PrivateTmp != "yes" || st.MountFlags != "slave" {
			t.Fatalf("%+v %v", st, err)
		}
	})
	bad := map[string]string{
		"missing key":       "RuntimeMaxUSec=1h 25min\nPrivateTmp=no\n",
		"duplicate key":     "RuntimeMaxUSec=1h\nRuntimeMaxUSec=2h\nPrivateTmp=no\nMountFlags=\n",
		"unknown key":       "RuntimeMaxUSec=1h\nPrivateTmp=no\nMountFlags=\nExecStart=/bin/true\n",
		"no equals":         "RuntimeMaxUSec 1h\nPrivateTmp=no\nMountFlags=\n",
		"infinity backstop": "RuntimeMaxUSec=infinity\nPrivateTmp=no\nMountFlags=\n",
		"empty":             "",
	}
	for name, out := range bad {
		t.Run("[OPS-001] rejects "+name, func(t *testing.T) {
			if _, err := parseUnitShow(out); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	t.Run("[OPS-001] the show argv is fixed", func(t *testing.T) {
		if got := strings.Join(unitShowArgs, " "); got != "show riot-maintenance.service -p RuntimeMaxUSec -p PrivateTmp -p MountFlags" {
			t.Errorf("argv = %q", got)
		}
		if unitShowPath != "/usr/bin/systemctl" {
			t.Errorf("path = %q", unitShowPath)
		}
	})
}

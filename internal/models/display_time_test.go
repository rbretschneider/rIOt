package models

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // named zones load the same on every machine (TIME-12H-001 AD-002)
)

func loadZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

// exampleInstant is 2026-10-27T17:05:09Z, 1:05:09 PM in New York (EDT).
var exampleInstant = time.Date(2026, 10, 27, 17, 5, 9, 0, time.UTC)

// tzifWithAbbreviation assembles a minimal TZif v1 blob with one zone type, so a
// test can hand Go a zone database whose abbreviation bytes are hostile. No
// binary fixture is committed.
func tzifWithAbbreviation(abbr string, offsetSeconds int32) []byte {
	chars := append([]byte(abbr), 0)
	var b []byte
	b = append(b, "TZif"...)
	b = append(b, 0)
	b = append(b, make([]byte, 15)...)
	counts := []uint32{0, 0, 0, 0, 1, uint32(len(chars))} // isutc, isstd, leap, time, type, char
	for _, c := range counts {
		b = binary.BigEndian.AppendUint32(b, c)
	}
	b = binary.BigEndian.AppendUint32(b, uint32(offsetSeconds))
	b = append(b, 0, 0) // isdst, abbreviation index
	b = append(b, chars...)
	return b
}

func TestTIME12H_AC063_ZoneLabelRules(t *testing.T) {
	cases := []struct {
		name string
		loc  *time.Location
		at   time.Time
		want string
	}{
		{"EDT in summer", loadZone(t, "America/New_York"), exampleInstant, "EDT"},
		{"EST in winter", loadZone(t, "America/New_York"), time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), "EST"},
		{"IST", loadZone(t, "Asia/Kolkata"), exampleInstant, "IST"},
		{"UTC", time.UTC, exampleInstant, "UTC"},
		{"GMT in London winter", loadZone(t, "Europe/London"), time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), "GMT"},
		{"BST in London summer", loadZone(t, "Europe/London"), time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), "BST"},
		{"numeric -03 becomes GMT-3", loadZone(t, "America/Sao_Paulo"), exampleInstant, "GMT-3"},
		{"numeric +0545 becomes GMT+5:45", loadZone(t, "Asia/Kathmandu"), exampleInstant, "GMT+5:45"},
		{"numeric +0530 becomes GMT+5:30", time.FixedZone("+0530", 5*3600+30*60), exampleInstant, "GMT+5:30"},
		{"numeric -0930 becomes GMT-9:30", time.FixedZone("-0930", -(9*3600 + 30*60)), exampleInstant, "GMT-9:30"},
		{"numeric +00 with zero offset is GMT", time.FixedZone("+00", 0), exampleInstant, "GMT"},
		{"numeric +01 becomes GMT+1", time.FixedZone("+01", 3600), exampleInstant, "GMT+1"},
		{"hours of two digits keep both", time.FixedZone("+14", 14*3600), exampleInstant, "GMT+14"},
		{"empty name falls back to the offset", time.FixedZone("", -4*3600), exampleInstant, "GMT-4"},
		{"non-letter name with a sub-minute offset is plain GMT", time.FixedZone("LMT1", 30), exampleInstant, "GMT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DisplayZoneLabel(tc.at.In(tc.loc))
			if got != tc.want {
				t.Errorf("DisplayZoneLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTIME12H_AC063_FormattedNumericAbbreviations(t *testing.T) {
	cases := []struct {
		zone string
		want string
	}{
		{"America/Sao_Paulo", "2026-10-27 2:05:09 PM GMT-3"},
		{"Asia/Kathmandu", "2026-10-27 10:50:09 PM GMT+5:45"},
		{"Asia/Kolkata", "2026-10-27 10:35:09 PM IST"},
	}
	for _, tc := range cases {
		t.Run(tc.zone, func(t *testing.T) {
			got := FormatDisplayFull(exampleInstant, loadZone(t, tc.zone))
			if got != tc.want {
				t.Errorf("FormatDisplayFull in %s = %q, want %q", tc.zone, got, tc.want)
			}
		})
	}
}

func TestTIME12H_SEC001_HostileAbbreviationFallsBackToOffset(t *testing.T) {
	const offset = -4 * 3600
	hostile := map[string]string{
		"CRLF header injection": "EDT\r\nX-Injected: 1",
		"slack mention":         "<!channel>",
		"space":                 "A B",
		"non-ASCII letter":      "ÉDT",
		"seven letters":         "ABCDEFG",
		"200 bytes":             strings.Repeat("A", 200),
	}
	for name, abbr := range hostile {
		t.Run(name, func(t *testing.T) {
			loc, err := time.LoadLocationFromTZData("Test/Hostile", tzifWithAbbreviation(abbr, offset))
			if err != nil {
				t.Fatalf("LoadLocationFromTZData: %v", err)
			}
			in := exampleInstant.In(loc)
			if got, _ := in.Zone(); got != abbr {
				t.Fatalf("test setup: zone database abbreviation = %q, want the hostile %q", got, abbr)
			}
			label := DisplayZoneLabel(in)
			if label != "GMT-4" {
				t.Errorf("label = %q, want the offset form GMT-4", label)
			}
			if len(label) > 9 {
				t.Errorf("label %q is %d bytes, want at most 9", label, len(label))
			}
			full := FormatDisplayFull(exampleInstant, loc)
			if want := "2026-10-27 1:05:09 PM GMT-4"; full != want {
				t.Errorf("FormatDisplayFull = %q, want %q", full, want)
			}
		})
	}

	t.Run("real abbreviations are unchanged", func(t *testing.T) {
		winter := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
		cases := []struct {
			zone string
			at   time.Time
			want string
		}{
			{"America/New_York", exampleInstant, "EDT"},
			{"America/New_York", winter, "EST"},
			{"Asia/Kolkata", exampleInstant, "IST"},
			{"UTC", exampleInstant, "UTC"},
			{"Europe/London", winter, "GMT"},
			{"Europe/London", time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), "BST"},
		}
		for _, tc := range cases {
			if got := DisplayZoneLabel(tc.at.In(loadZone(t, tc.zone))); got != tc.want {
				t.Errorf("%s label = %q, want %q", tc.zone, got, tc.want)
			}
		}
	})
}

func TestTIME12H_AC067_NilZoneFormatsUTCWithoutPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a nil zone panicked: %v", r)
		}
	}()
	if got, want := FormatDisplayFull(exampleInstant, nil), "2026-10-27 5:05:09 PM UTC"; got != want {
		t.Errorf("FormatDisplayFull(nil zone) = %q, want %q", got, want)
	}
	if got, want := FormatDisplayMinute(exampleInstant, nil), "2026-10-27 5:05 PM UTC"; got != want {
		t.Errorf("FormatDisplayMinute(nil zone) = %q, want %q", got, want)
	}
	if got, want := FormatDisplayClock(exampleInstant, nil), "5:05 PM UTC"; got != want {
		t.Errorf("FormatDisplayClock(nil zone) = %q, want %q", got, want)
	}
	if got, want := FormatDisplayDate(exampleInstant, nil), "2026-10-27"; got != want {
		t.Errorf("FormatDisplayDate(nil zone) = %q, want %q", got, want)
	}
}

func TestTIME12H_AC062_FormatHelpersInNewYork(t *testing.T) {
	ny := loadZone(t, "America/New_York")
	if got, want := FormatDisplayFull(exampleInstant, ny), "2026-10-27 1:05:09 PM EDT"; got != want {
		t.Errorf("full = %q, want %q", got, want)
	}
	if got, want := FormatDisplayMinute(exampleInstant, ny), "2026-10-27 1:05 PM EDT"; got != want {
		t.Errorf("minute = %q, want %q", got, want)
	}
	if got, want := FormatDisplayClock(exampleInstant, ny), "1:05 PM EDT"; got != want {
		t.Errorf("clock = %q, want %q", got, want)
	}
	if got, want := FormatDisplayDate(exampleInstant, ny), "2026-10-27"; got != want {
		t.Errorf("date = %q, want %q", got, want)
	}
}

func TestTIME12H_AC062_MidnightNoonAndDSTClock(t *testing.T) {
	ny := loadZone(t, "America/New_York")
	cases := []struct {
		at   string
		want string
	}{
		{"2026-10-27T04:00:00Z", "12:00 AM EDT"},
		{"2026-10-27T16:00:00Z", "12:00 PM EDT"},
		{"2026-11-01T05:30:00Z", "1:30 AM EDT"},
		{"2026-11-01T06:30:00Z", "1:30 AM EST"},
		{"2026-03-08T06:59:59Z", "1:59 AM EST"},
		{"2026-03-08T07:00:00Z", "3:00 AM EDT"},
	}
	for _, tc := range cases {
		at, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		if got := FormatDisplayClock(at, ny); got != tc.want {
			t.Errorf("FormatDisplayClock(%s) = %q, want %q", tc.at, got, tc.want)
		}
	}
}

func TestTIME12H_AC054_DateInDisplayZoneDiffersFromUTC(t *testing.T) {
	at := time.Date(2026, 10, 28, 2, 0, 0, 0, time.UTC)
	if got, want := FormatDisplayDate(at, loadZone(t, "America/New_York")), "2026-10-27"; got != want {
		t.Errorf("display-zone date = %q, want %q (UTC date is 2026-10-28)", got, want)
	}
}

func TestTIME12H_AC067_HelpersDoNotMutateTheirInput(t *testing.T) {
	at := exampleInstant
	_ = FormatDisplayFull(at, loadZone(t, "Asia/Tokyo"))
	if at.Location() != time.UTC {
		t.Errorf("caller's time location changed to %v, want UTC", at.Location())
	}
	if !at.Equal(exampleInstant) {
		t.Errorf("caller's instant changed to %v", at)
	}
}

func TestTIME12H_SEC001_LabelPropertyHoldsForEveryOffset(t *testing.T) {
	for offset := -14 * 3600; offset <= 14*3600; offset += 15 * 60 {
		label := DisplayZoneLabel(exampleInstant.In(time.FixedZone("+99", offset)))
		if len(label) > 9 || !strings.HasPrefix(label, "GMT") {
			t.Errorf("offset %d gave label %q, want GMT[+-H[:MM]] of at most 9 bytes", offset, label)
		}
		if strings.ContainsAny(label[3:], "\r\n <>") {
			t.Errorf("offset %d gave label %q with a forbidden byte", offset, label)
		}
	}
}

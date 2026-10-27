package models

import (
	"strconv"
	"time"
)

// Server-written human text shows time in the server display zone, in 12-hour
// form, with a zone label (TIME-12H-001 AD-003). Every renderer goes through
// these helpers so the layouts cannot drift apart. Stored data and API fields
// stay UTC; only text for a person is converted.

const (
	displayDateLayout   = "2006-01-02"
	displayClockLayout  = "3:04 PM"
	displayMinuteLayout = displayDateLayout + " " + displayClockLayout
	displayFullLayout   = displayDateLayout + " 3:04:05 PM"
)

// FormatDisplayFull renders t as "2026-10-27 1:05:09 PM EDT" in loc. A nil loc
// means UTC.
func FormatDisplayFull(t time.Time, loc *time.Location) string {
	return formatDisplay(t, loc, displayFullLayout)
}

// FormatDisplayMinute renders t as "2026-10-27 1:05 PM EDT" in loc.
func FormatDisplayMinute(t time.Time, loc *time.Location) string {
	return formatDisplay(t, loc, displayMinuteLayout)
}

// FormatDisplayClock renders the time of day only, "1:05 PM EDT". The label
// settles the repeated hour of a DST fall-back.
func FormatDisplayClock(t time.Time, loc *time.Location) string {
	return formatDisplay(t, loc, displayClockLayout)
}

// FormatDisplayDate renders the calendar date of t in loc, "2026-10-27", with
// no zone label (the date-only exception to labelled times).
func FormatDisplayDate(t time.Time, loc *time.Location) string {
	return inDisplayZone(t, loc).Format(displayDateLayout)
}

func formatDisplay(t time.Time, loc *time.Location, layout string) string {
	in := inDisplayZone(t, loc)
	return in.Format(layout) + " " + DisplayZoneLabel(in)
}

// inDisplayZone converts a copy of t; the caller's value is never modified and
// t.In(nil) (which panics) is never called.
func inDisplayZone(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc)
}

// DisplayZoneLabel is the zone label for t in its own location.
//
// The zone database abbreviation is used only when it is 1 to 6 ASCII letters
// (EDT, IST, UTC). Go's TZif parser does not constrain abbreviation bytes, and
// the label reaches notification channels and a Markdown file, so anything
// else (numeric "-03", empty, longer, any other byte) gets the offset form:
// "GMT", then the sign, the hours without a leading zero, and ":MM" only when
// the minutes are not zero. The label is therefore always 1 to 6 letters or
// at most 9 bytes of GMT[+-H[:MM]] whatever the zone database holds.
func DisplayZoneLabel(t time.Time) string {
	name, offset := t.Zone()
	if isZoneAbbreviation(name) {
		return name
	}
	return offsetZoneLabel(offset)
}

func isZoneAbbreviation(s string) bool {
	if len(s) < 1 || len(s) > 6 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

// offsetZoneLabel builds GMT[+-H[:MM]] from an offset in seconds. Seconds are
// dropped; they only occur in historical LMT offsets. A zero offset is plain
// "GMT" to match the en-US browser form.
func offsetZoneLabel(offset int) string {
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	if offset < 60 {
		return "GMT"
	}
	label := "GMT" + sign + strconv.Itoa(offset/3600)
	if m := offset % 3600 / 60; m != 0 {
		label += ":" + twoDigits(m)
	}
	return label
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

package handlers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Reference calendar used below (all UTC, verified): 2026-10-15 is a Thursday;
// 2026-10-31 is a Saturday (last day of October).

func utc(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, time.UTC)
}

func windowCfg(start, end, freq string, days ...int) models.MaintenanceWindow {
	return models.MaintenanceWindow{
		Mode:            "window",
		StartTime:       start,
		EndTime:         end,
		CooldownMinutes: 60,
		Frequency:       freq,
		DaysOfWeek:      days,
	}
}

// [AC-013] Same-day window returns today's start when now is before it.
func TestNextMaintenanceWindowStart_AC013_SameDayBeforeStartReturnsToday(t *testing.T) {
	w := windowCfg("09:00", "17:00", "daily")
	now := utc(2026, time.October, 15, 6, 30)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok, "[AC-013] a same-day daily window must be found")
	assert.Equal(t, utc(2026, time.October, 15, 9, 0), got, "[AC-013] must return the same day's 09:00 UTC start")
	assert.Equal(t, time.UTC, got.Location(), "[AC-013] result must be UTC")
}

// [AC-014] Overnight window: start instant is the start-day start time.
func TestNextMaintenanceWindowStart_AC014_OvernightReturnsStartDayStart(t *testing.T) {
	w := windowCfg("23:00", "05:00", "daily")
	now := utc(2026, time.October, 15, 10, 0)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok, "[AC-014] an overnight daily window must be found")
	assert.Equal(t, utc(2026, time.October, 15, 23, 0), got, "[AC-014] must return the 23:00 start on the start day, not the 05:00 end")
}

// [AC-014] Overnight window, now in the post-midnight tail of the previous
// night's window: today's 23:00 start is still the next start.
func TestNextMaintenanceWindowStart_AC014_OvernightTailResolvesToTonightsStart(t *testing.T) {
	w := windowCfg("23:00", "05:00", "daily")
	now := utc(2026, time.October, 16, 2, 0)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok)
	assert.Equal(t, utc(2026, time.October, 16, 23, 0), got, "[AC-014] at 02:00 (inside last night's window) the next start is tonight's 23:00")
}

// [AC-015] Weekly: next listed weekday on or after now.
func TestNextMaintenanceWindowStart_AC015_WeeklyReturnsNextListedDay(t *testing.T) {
	// Thursday 2026-10-15; eligible Mon(1) and Wed(3) -> next is Mon 2026-10-19.
	w := windowCfg("02:00", "04:00", "weekly", 1, 3)
	now := utc(2026, time.October, 15, 10, 0)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok, "[AC-015] weekly with listed days must be found")
	assert.Equal(t, utc(2026, time.October, 19, 2, 0), got, "[AC-015] must return Monday's start")
	assert.Equal(t, time.Monday, got.Weekday(), "[AC-015] result must land on a listed weekday")
}

// [AC-015] Weekly: today is eligible and the start is still ahead.
func TestNextMaintenanceWindowStart_AC015_WeeklyTodayEligibleBeforeStartReturnsToday(t *testing.T) {
	w := windowCfg("20:00", "22:00", "weekly", int(time.Thursday))
	now := utc(2026, time.October, 15, 10, 0)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok)
	assert.Equal(t, utc(2026, time.October, 15, 20, 0), got, "[AC-015] eligible today with start ahead must return today")
}

// [AC-015] Weekly: today is eligible but its start already passed -> next week.
func TestNextMaintenanceWindowStart_AC015_WeeklyTodayEligibleAfterStartReturnsNextWeek(t *testing.T) {
	w := windowCfg("02:00", "04:00", "weekly", int(time.Thursday))
	now := utc(2026, time.October, 15, 10, 0)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok)
	assert.Equal(t, utc(2026, time.October, 22, 2, 0), got, "[AC-015] passed start today must roll to the same weekday next week")
}

// [AC-016] Monthly: last calendar day of the month.
func TestNextMaintenanceWindowStart_AC016_MonthlyReturnsLastDayOfMonth(t *testing.T) {
	w := windowCfg("03:00", "05:00", "monthly")
	now := utc(2026, time.October, 15, 10, 0)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok, "[AC-016] monthly must be found")
	assert.Equal(t, utc(2026, time.October, 31, 3, 0), got, "[AC-016] must be the last day of October")
}

// [AC-016] Monthly: after the last day's start, rolls to the next month's last day.
func TestNextMaintenanceWindowStart_AC016_MonthlyAfterLastDayStartRollsToNextMonth(t *testing.T) {
	w := windowCfg("03:00", "05:00", "monthly")
	now := utc(2026, time.October, 31, 12, 0)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok)
	assert.Equal(t, utc(2026, time.November, 30, 3, 0), got, "[AC-016] must roll to November 30 (30-day month)")
}

// [AC-016] Monthly in a leap-year February resolves to the 29th.
func TestNextMaintenanceWindowStart_AC016_MonthlyLeapFebruary(t *testing.T) {
	w := windowCfg("03:00", "05:00", "monthly")
	now := utc(2028, time.February, 10, 0, 0)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok)
	assert.Equal(t, utc(2028, time.February, 29, 3, 0), got, "[AC-016] leap February last day is the 29th")
}

// [AC-017] Daily after today's start -> tomorrow's start.
func TestNextMaintenanceWindowStart_AC017_DailyAfterStartReturnsTomorrow(t *testing.T) {
	w := windowCfg("02:00", "04:00", "daily")
	now := utc(2026, time.October, 15, 10, 0)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok, "[AC-017] daily must be found")
	assert.Equal(t, utc(2026, time.October, 16, 2, 0), got, "[AC-017] must return tomorrow's start")
}

// [AC-017] Empty frequency is treated as daily (back-compat).
func TestNextMaintenanceWindowStart_AC017_EmptyFrequencyIsDaily(t *testing.T) {
	w := windowCfg("02:00", "04:00", "")
	now := utc(2026, time.October, 15, 10, 0)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok)
	assert.Equal(t, utc(2026, time.October, 16, 2, 0), got, "[AC-017] empty frequency behaves as daily")
}

// [AC-018] Strictly inside an active window -> next eligible occurrence (AD-003).
func TestNextMaintenanceWindowStart_AC018_InsideWindowReturnsNextOccurrence(t *testing.T) {
	w := windowCfg("09:00", "17:00", "daily")
	now := utc(2026, time.October, 15, 12, 0)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok)
	assert.Equal(t, utc(2026, time.October, 16, 9, 0), got, "[AC-018] inside a window the past start is skipped; result is tomorrow's start")
}

// [AC-018] One minute after the start is already "inside" -> next occurrence.
func TestNextMaintenanceWindowStart_AC018_OneMinuteAfterStartReturnsNextOccurrence(t *testing.T) {
	w := windowCfg("09:00", "17:00", "daily")
	now := utc(2026, time.October, 15, 9, 1)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok)
	assert.Equal(t, utc(2026, time.October, 16, 9, 0), got, "[AC-018] 09:01 is past the 09:00 start")
}

// [AC-018] now == start exactly -> that start is returned (inclusive bound).
func TestNextMaintenanceWindowStart_AC018_ExactStartInstantReturnsNow(t *testing.T) {
	w := windowCfg("09:00", "17:00", "daily")
	now := utc(2026, time.October, 15, 9, 0)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok)
	assert.Equal(t, now, got, "[AC-018] exact start instant must return now (>= bound is inclusive)")
}

// [AC-018] Sub-minute precision: 09:00:30 is past a 09:00 start.
func TestNextMaintenanceWindowStart_AC018_SecondsAfterStartIsPast(t *testing.T) {
	w := windowCfg("09:00", "17:00", "daily")
	now := time.Date(2026, time.October, 15, 9, 0, 30, 0, time.UTC)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok)
	assert.Equal(t, utc(2026, time.October, 16, 9, 0), got, "[AC-018] 30s after start is past it")
}

// [AC-019] / SEC-004: not-found cases return the zero value, never a partial time.
func TestNextMaintenanceWindowStart_AC019_NotFoundCases(t *testing.T) {
	now := utc(2026, time.October, 15, 10, 0)
	cases := map[string]models.MaintenanceWindow{
		"invalid start (not HH:MM)":      windowCfg("garbage", "05:00", "daily"),
		"invalid start (hour 24)":        windowCfg("24:00", "05:00", "daily"),
		"invalid start (minute 60)":      windowCfg("12:60", "05:00", "daily"),
		"empty start":                    windowCfg("", "05:00", "daily"),
		"weekly with empty days":         windowCfg("02:00", "04:00", "weekly"),
		"weekly with out-of-range days":  windowCfg("02:00", "04:00", "weekly", 9, -1),
		"mode anytime":                   {Mode: "anytime", StartTime: "02:00", EndTime: "04:00"},
		"mode disabled":                  {Mode: "disabled", StartTime: "02:00", EndTime: "04:00"},
		"mode empty":                     {StartTime: "02:00", EndTime: "04:00"},
		"mode unknown":                   {Mode: "bogus", StartTime: "02:00", EndTime: "04:00"},
	}
	for name, w := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := nextMaintenanceWindowStart(w, now)
			assert.False(t, ok, "[AC-019] %s must report not-found", name)
			assert.True(t, got.IsZero(), "[AC-019] %s must return the zero time, got %v", name, got)
		})
	}
}

// [AC-021] The reference instant is injected: results follow the injected
// clock, not the wall clock, and are independent of the input's location.
func TestNextMaintenanceWindowStart_AC021_FollowsInjectedClock(t *testing.T) {
	w := windowCfg("09:00", "17:00", "daily")

	past, ok := nextMaintenanceWindowStart(w, utc(2001, time.March, 4, 1, 0))
	assert.True(t, ok)
	assert.Equal(t, utc(2001, time.March, 4, 9, 0), past, "[AC-021] a year-2001 clock must yield a year-2001 result")

	future, ok := nextMaintenanceWindowStart(w, utc(2090, time.July, 9, 1, 0))
	assert.True(t, ok)
	assert.Equal(t, utc(2090, time.July, 9, 9, 0), future, "[AC-021] a year-2090 clock must yield a year-2090 result")
}

// [AC-021] A non-UTC `now` is normalised to UTC before the day is derived.
func TestNextMaintenanceWindowStart_AC021_NonUTCInputIsNormalised(t *testing.T) {
	w := windowCfg("09:00", "17:00", "daily")
	// 2026-10-16 02:00 at UTC+5 is 2026-10-15 21:00 UTC -> after 09:00 -> tomorrow.
	zone := time.FixedZone("UTC+5", 5*3600)
	now := time.Date(2026, time.October, 16, 2, 0, 0, 0, zone)

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok)
	assert.Equal(t, utc(2026, time.October, 16, 9, 0), got, "[AC-021] day must be derived from the UTC instant, not the local wall date")
}

// [AC-021] Deterministic: identical inputs give identical output.
func TestNextMaintenanceWindowStart_AC021_Deterministic(t *testing.T) {
	w := windowCfg("02:00", "04:00", "weekly", 0, 6)
	now := utc(2026, time.October, 15, 10, 0)

	first, ok1 := nextMaintenanceWindowStart(w, now)
	second, ok2 := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok1 && ok2)
	assert.Equal(t, first, second, "[AC-021] repeated calls with the same clock must agree")
}

// SEC-004: a weekly rule satisfiable only on a day within the horizon is found,
// proving the 366-day cap does not truncate legitimate recurrences.
func TestNextMaintenanceWindowStart_AC015_WeeklySundayOnlyWithinHorizon(t *testing.T) {
	w := windowCfg("02:00", "04:00", "weekly", int(time.Sunday))
	now := utc(2026, time.October, 15, 10, 0) // Thursday

	got, ok := nextMaintenanceWindowStart(w, now)

	assert.True(t, ok)
	assert.Equal(t, utc(2026, time.October, 18, 2, 0), got, "[AC-015] next Sunday is 2026-10-18")
}

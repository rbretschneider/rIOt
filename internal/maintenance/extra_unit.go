package maintenance

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Properties the worker reads from systemd about its own unit (AD-005 check 6).
// The argv is fixed; systemd answers in unit-property order.
var unitShowArgs = []string{"show", UnitName, "-p", "RuntimeMaxUSec", "-p", "PrivateTmp", "-p", "MountFlags"}

const (
	unitShowPath     = "/usr/bin/systemctl"
	unitShowBound    = 4096
	unitShowTimeout  = 5 * time.Second
	maxTimespanCount = 1_000_000_000
)

// parseUnitShow parses `systemctl show` output with a closed grammar: exactly
// the three requested keys, each once.
func parseUnitShow(out string) (UnitState, error) {
	var state UnitState
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || seen[key] {
			return UnitState{}, fmt.Errorf("unexpected systemctl line %q", boundedText(line, 80))
		}
		seen[key] = true
		switch key {
		case "RuntimeMaxUSec":
			d, err := parseSystemdTimespan(value)
			if err != nil {
				return UnitState{}, err
			}
			state.RuntimeMax = d
		case "PrivateTmp":
			state.PrivateTmp = value
		case "MountFlags":
			state.MountFlags = value
		default:
			return UnitState{}, fmt.Errorf("unexpected systemctl property %q", boundedText(key, 40))
		}
	}
	if len(seen) != 3 {
		return UnitState{}, errors.New("systemctl did not report all of RuntimeMaxUSec, PrivateTmp and MountFlags")
	}
	return state, nil
}

var timespanUnits = map[string]time.Duration{
	"us":  time.Microsecond,
	"ms":  time.Millisecond,
	"s":   time.Second,
	"min": time.Minute,
	"h":   time.Hour,
	"d":   24 * time.Hour,
}

// parseSystemdTimespan parses the renderings systemd uses for RuntimeMaxUSec: a
// bare number of microseconds ("5100000000") or space-separated integer parts
// ("1h 25min", "35min", "1min 30s"). Anything else, including "infinity", is an
// error, so an unreadable backstop is never mistaken for a long one (SEC-002).
func parseSystemdTimespan(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty timespan")
	}
	if allDigits(s) {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n > int64(1<<62)/int64(time.Microsecond) {
			return 0, errors.New("timespan out of range")
		}
		return time.Duration(n) * time.Microsecond, nil
	}
	var total time.Duration
	used := map[string]bool{}
	for _, part := range strings.Fields(s) {
		i := 0
		for i < len(part) && part[i] >= '0' && part[i] <= '9' {
			i++
		}
		if i == 0 || i == len(part) {
			return 0, fmt.Errorf("unsupported timespan %q", boundedText(s, 60))
		}
		unit, ok := timespanUnits[part[i:]]
		n, err := strconv.ParseInt(part[:i], 10, 64)
		if !ok || err != nil || n > maxTimespanCount || used[part[i:]] {
			return 0, fmt.Errorf("unsupported timespan %q", boundedText(s, 60))
		}
		used[part[i:]] = true
		total += time.Duration(n) * unit
		if total < 0 {
			return 0, errors.New("timespan out of range")
		}
	}
	return total, nil
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

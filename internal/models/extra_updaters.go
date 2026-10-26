package models

import (
	"bytes"
	"encoding/json"
	"regexp"
)

// Extra updaters (EXTRA-UPD-001): platform updaters that work outside the
// package manager, such as dietpi-update. The agent reports each present one in
// updates telemetry; the types here are the wire shape and its bounds.

// ExtraUpdaterIDPattern is the only accepted shape of an updater identifier
// (FR-002).
var ExtraUpdaterIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// ExtraUpdaterDietPi is the identifier of the only updater built in this story.
const ExtraUpdaterDietPi = "dietpi"

// Bounds on agent-supplied extra-updater telemetry (NFR-002, NFR-006). The
// server enforces them on ingest and on every read.
const (
	MaxExtraUpdaters       = 8
	MaxExtraUpdaterNotices = 8
	MaxExtraUpdaterVersion = 32
	MaxExtraUpdaterString  = 64
	MaxExtraUpdaterItems   = 50
	MaxExtraUpdaterPending = 10000
	// ExtraUpdaterUnknownValue replaces any status, reason or readiness the
	// server does not recognise (FR-047).
	ExtraUpdaterUnknownValue = "unknown"
	extraUpdaterIDMaxBytes   = 32
)

// ExtraUpdaterStatus is the detected update state of one updater.
type ExtraUpdaterStatus string

const (
	ExtraStatusUpdateAvailable ExtraUpdaterStatus = "update_available"
	ExtraStatusUpToDate        ExtraUpdaterStatus = "up_to_date"
	ExtraStatusUnknown         ExtraUpdaterStatus = "unknown"
)

// Reasons a status is unknown (FRD section 7).
const (
	ExtraReasonCheckDisabled       = "check_disabled"
	ExtraReasonConfigUnreadable    = "config_unreadable"
	ExtraReasonNotCheckedSinceBoot = "not_checked_since_boot"
	ExtraReasonVersionUnreadable   = "version_unreadable"
	ExtraReasonDetectionFailed     = "detection_failed"
)

// Apply readiness values (FR-024, AF-8).
const (
	ExtraReadinessEnabled                = "enabled"
	ExtraReadinessNotOptedIn             = "not_opted_in"
	ExtraReadinessRootSideMissing        = "root_side_missing"
	ExtraReadinessPatchingDisabled       = "patching_disabled"
	ExtraReadinessUnsupportedHelper      = "unsupported_helper"
	ExtraReadinessNonDefaultUpdateSource = "non_default_update_source"
)

// Notice codes (FR-011 to FR-014) and the placeholder for an unprintable value.
const (
	ExtraNoticeLivePatchesAvailable   = "live_patches_available"
	ExtraNoticeDietPiAutoAptUpgrade   = "dietpi_auto_apt_upgrade"
	ExtraNoticeNonDefaultUpdateSource = "non_default_update_source"
	ExtraNoticeMajorUpgrade           = "major_upgrade"
	ExtraNoticeValueUnreadable        = "unreadable"
)

// ExtraUpdaterNotice is one fixed-code notice. Branch and Owner are set only
// for non_default_update_source.
type ExtraUpdaterNotice struct {
	Code   string `json:"code"`
	Branch string `json:"branch,omitempty"`
	Owner  string `json:"owner,omitempty"`
}

// ExtraUpdater is one detected extra updater (FRD section 7). The detector
// leaves Readiness empty; the agent stamps it before sending.
type ExtraUpdater struct {
	ID               string               `json:"id"`
	Status           ExtraUpdaterStatus   `json:"status"`
	UnknownReason    string               `json:"unknown_reason,omitempty"`
	InstalledVersion string               `json:"installed_version,omitempty"`
	AvailableVersion string               `json:"available_version,omitempty"`
	PendingCount     int                  `json:"pending_count"`
	PendingItems     []string             `json:"pending_items,omitempty"`
	Readiness        string               `json:"readiness,omitempty"`
	Notices          []ExtraUpdaterNotice `json:"notices,omitempty"`
}

// HasNotice reports whether the entry carries the notice code.
func (e ExtraUpdater) HasNotice(code string) bool {
	for _, n := range e.Notices {
		if n.Code == code {
			return true
		}
	}
	return false
}

// Pending reports whether the entry has an update waiting.
func (e ExtraUpdater) Pending() bool { return e.Status == ExtraStatusUpdateAvailable }

// Enabled reports whether the agent says it would apply the updater.
func (e ExtraUpdater) Enabled() bool { return e.Readiness == ExtraReadinessEnabled }

// ExtraUpdaterList is the lenient decoder for updates.extra_updaters. It never
// returns an error and never logs, so a hostile or future payload cannot reject
// a telemetry ingest (FR-047).
type ExtraUpdaterList []ExtraUpdater

// UnmarshalJSON normalises the payload: a non-array or null becomes nil; at most
// MaxExtraUpdaters entries are kept in input order; entries with a bad or
// duplicate ID are dropped; unknown enum values become "unknown"; strings are
// sanitised then cut to their bounds.
func (l *ExtraUpdaterList) UnmarshalJSON(data []byte) error {
	*l = nil
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(trimmed, &raws); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out ExtraUpdaterList
	for _, raw := range raws {
		if len(out) >= MaxExtraUpdaters {
			break
		}
		entry, ok := decodeExtraUpdater(raw)
		if !ok || seen[entry.ID] {
			continue
		}
		seen[entry.ID] = true
		out = append(out, entry)
	}
	*l = out
	return nil
}

// extraUpdaterWire mirrors ExtraUpdater with loosely typed fields, so one badly
// typed field degrades to a default instead of rejecting the entry.
type extraUpdaterWire struct {
	ID               json.RawMessage `json:"id"`
	Status           json.RawMessage `json:"status"`
	UnknownReason    json.RawMessage `json:"unknown_reason"`
	InstalledVersion json.RawMessage `json:"installed_version"`
	AvailableVersion json.RawMessage `json:"available_version"`
	PendingCount     json.RawMessage `json:"pending_count"`
	PendingItems     json.RawMessage `json:"pending_items"`
	Readiness        json.RawMessage `json:"readiness"`
	Notices          json.RawMessage `json:"notices"`
}

func decodeExtraUpdater(raw json.RawMessage) (ExtraUpdater, bool) {
	var w extraUpdaterWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return ExtraUpdater{}, false
	}
	var e ExtraUpdater
	e.ID = wireString(w.ID)
	if len(e.ID) > extraUpdaterIDMaxBytes || !ExtraUpdaterIDPattern.MatchString(e.ID) {
		return ExtraUpdater{}, false
	}
	switch s := ExtraUpdaterStatus(wireString(w.Status)); s {
	case ExtraStatusUpdateAvailable, ExtraStatusUpToDate, ExtraStatusUnknown:
		e.Status = s
	default:
		e.Status = ExtraStatusUnknown
	}
	switch r := wireString(w.UnknownReason); r {
	case "":
	case ExtraReasonCheckDisabled, ExtraReasonConfigUnreadable, ExtraReasonNotCheckedSinceBoot,
		ExtraReasonVersionUnreadable, ExtraReasonDetectionFailed:
		e.UnknownReason = r
	default:
		e.UnknownReason = ExtraUpdaterUnknownValue
	}
	e.InstalledVersion = cutExtraString(wireString(w.InstalledVersion), MaxExtraUpdaterVersion)
	e.AvailableVersion = cutExtraString(wireString(w.AvailableVersion), MaxExtraUpdaterVersion)
	e.PendingCount = wireClampedInt(w.PendingCount, MaxExtraUpdaterPending)
	e.PendingItems = decodeExtraItems(w.PendingItems)
	switch r := wireString(w.Readiness); r {
	case "":
	case ExtraReadinessEnabled, ExtraReadinessNotOptedIn, ExtraReadinessRootSideMissing,
		ExtraReadinessPatchingDisabled, ExtraReadinessUnsupportedHelper, ExtraReadinessNonDefaultUpdateSource:
		e.Readiness = r
	default:
		e.Readiness = ExtraUpdaterUnknownValue
	}
	e.Notices = decodeExtraNotices(w.Notices)
	return e, true
}

func wireString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

func wireClampedInt(raw json.RawMessage, max int) int {
	var f float64
	if len(raw) == 0 || json.Unmarshal(raw, &f) != nil || f != f || f < 0 {
		return 0
	}
	if f > float64(max) {
		return max
	}
	return int(f)
}

func cutExtraString(s string, n int) string {
	return TruncateHead(SanitizeText(s), n)
}

func decodeExtraItems(raw json.RawMessage) []string {
	var items []string
	if len(raw) == 0 || json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var out []string
	for _, it := range items {
		if len(out) >= MaxExtraUpdaterItems {
			break
		}
		out = append(out, cutExtraString(it, MaxExtraUpdaterString))
	}
	return out
}

func decodeExtraNotices(raw json.RawMessage) []ExtraUpdaterNotice {
	var notices []ExtraUpdaterNotice
	if len(raw) == 0 || json.Unmarshal(raw, &notices) != nil {
		return nil
	}
	var out []ExtraUpdaterNotice
	for _, n := range notices {
		if len(out) >= MaxExtraUpdaterNotices {
			break
		}
		switch n.Code {
		case ExtraNoticeLivePatchesAvailable, ExtraNoticeDietPiAutoAptUpgrade,
			ExtraNoticeNonDefaultUpdateSource, ExtraNoticeMajorUpgrade:
		default:
			continue
		}
		out = append(out, ExtraUpdaterNotice{
			Code:   n.Code,
			Branch: cutExtraString(n.Branch, MaxExtraUpdaterString),
			Owner:  cutExtraString(n.Owner, MaxExtraUpdaterString),
		})
	}
	return out
}

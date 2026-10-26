// Package maintenance defines the closed protocol shared by the unprivileged
// observer and the independent privileged maintenance service.
package maintenance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

const (
	ProtocolVersion    = 1
	InboxPath          = "/var/lib/riot/maintenance-request.json"
	StateDir           = "/var/lib/riot-maintenance"
	LockPath           = "/run/lock/riot-maintenance.lock"
	HelperPath         = "/usr/local/lib/riot/riot-maintenance"
	UnitName           = "riot-maintenance.service"
	UnitPath           = "/etc/systemd/system/riot-maintenance.service"
	MaxRequestBytes    = 4096
	MaxOutputBytes     = 256 << 10
	MaxRecordBytes     = 2 << 20
	MaxTerminalRecords = 32
	MaxHistoryBytes    = 16 << 20
	WorkerBudget       = 30 * time.Minute
	CleanupBudget      = 2 * time.Minute
	TerminationGrace   = 90 * time.Second
)

// Extra-updater step (EXTRA-UPD-001). ProtocolVersion stays 1 because it is
// shared with UpdateStatus and `riot-agent-apply --maintenance-protocol`; the
// request and record gain a second version, RequestVersionExtra, that only a
// helper which advertises it may be sent (AD-002).
const (
	RequestVersionExtra = 2

	// ExtendedWorkerBudget is the overall worker budget of a v2 run. The apt part
	// keeps WorkerBudget (30m) plus CleanupBudget (2m); the DietPi step needs the
	// rest (AD-006).
	ExtendedWorkerBudget = 80 * time.Minute
	// ExtraStartThreshold is the extended budget that must remain before an
	// updater step may start: step limit 40m + grace 90s + lock wait 5m + 60s
	// margin.
	ExtraStartThreshold = 47*time.Minute + 30*time.Second
	// ExtraLockWait bounds the wait for the dpkg lock and for a busy DietPi
	// program before the step (FR-033, AD-008).
	ExtraLockWait = 5 * time.Minute
	// ExtraLockPoll is the poll interval of that wait.
	ExtraLockPoll = 5 * time.Second
	// ExtraMinInterval is the least time between two DietPi starts on a host
	// (SEC-006, D-11).
	ExtraMinInterval = 60 * time.Minute
	// ExtraServiceBackstop is the RuntimeMaxSec the installer writes into the
	// drop-in of an opted-in host. The worker refuses a step unless the unit's
	// effective backstop is at least ExtraMinBackstop (SEC-002).
	ExtraServiceBackstop = 85 * time.Minute
	ExtraMinBackstop     = 83*time.Minute + 30*time.Second
	// ExtraLauncherSetupBound bounds the launcher from start to its ok line.
	ExtraLauncherSetupBound = 10 * time.Second

	maxUpdaterNote    = 256
	maxUpdaterVersion = 32
	maxUpdaterSource  = 64
)

// Updater outcomes recorded per updater.
const (
	UpdaterRunning     = "running"
	UpdaterSuccess     = "success"
	UpdaterFailed      = "failed"
	UpdaterTimedOut    = "timed_out"
	UpdaterInterrupted = "interrupted"
	UpdaterSkipped     = "skipped"
)

// Skip reasons. A skip never changes the outcome of a successful apt run: the
// updater did not start, so nothing on the host changed (DIETPI-PATH-001 FR-012,
// amending EXTRA-UPD-001 FR-038). The first group is neutral and quiet; every
// other reason is a refusal, which raises a warning event and a badge item.
const (
	SkipAptNotSuccessful      = "apt_not_successful"
	SkipNotEnabled            = "not_enabled"
	SkipStatusUnknown         = "status_unknown"
	SkipNothingPending        = "nothing_pending"
	SkipInsufficientTime      = "insufficient_time"
	SkipNonDefaultSource      = "non_default_update_source"
	SkipUnsafePath            = "unsafe_dietpi_path"
	SkipMinInterval           = "min_interval"
	SkipTmpNotPrivate         = "tmp_not_private"
	SkipMountPropagationUnver = "mount_propagation_unverified"
	SkipBusy                  = "dietpi_busy"
	SkipDpkgLocked            = "dpkg_locked"
	SkipRunDirNotPrivate      = "rundir_not_private"
)

// Phases and apt outcomes of a v2 record.
const (
	phasePrefixExtraRun  = "extra_run:"
	phasePrefixExtraWait = "extra_wait:"
	aptOutcomeSuccess    = "success"
	aptOutcomeError      = "error"
	aptOutcomeInterrupt  = "interrupted"
)

var skipReasons = map[string]bool{
	SkipAptNotSuccessful: true, SkipNotEnabled: true, SkipStatusUnknown: true, SkipNothingPending: true,
	SkipInsufficientTime: true, SkipNonDefaultSource: true, SkipUnsafePath: true, SkipMinInterval: true,
	SkipTmpNotPrivate: true, SkipMountPropagationUnver: true, SkipBusy: true, SkipDpkgLocked: true,
	SkipRunDirNotPrivate: true,
}

// SkipReasons returns every known skip reason in sorted order. The server uses
// it as the closed set an agent event's reason must belong to (DIETPI-PATH-001
// FR-014).
func SkipReasons() []string {
	out := make([]string, 0, len(skipReasons))
	for r := range skipReasons {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// NeutralSkip reports whether a skip reason leaves a successful run successful.
func NeutralSkip(reason string) bool {
	return reason == SkipNotEnabled || reason == SkipStatusUnknown || reason == SkipNothingPending
}

var (
	ErrBusy        = errors.New("maintenance exclusion is busy")
	ErrUnsupported = errors.New("detached maintenance requires supported Linux apt/systemd installation")
	ErrHistoryFull = errors.New("maintenance history capacity reached; root operator must archive reviewed records")
)

// Request is the closed inbox request. Version 1 has exactly four fields. Version
// 2 (RequestVersionExtra) adds ExtraUpdaters, the updaters the agent asks the
// worker to apply; the worker intersects the ask with its root-owned policy, so
// the request can only ask, never grant (AD-002).
type Request struct {
	Version            int      `json:"version"`
	CommandID          string   `json:"command_id"`
	Mode               string   `json:"mode"`
	IncludeRebootClass bool     `json:"include_reboot_class"`
	ExtraUpdaters      []string `json:"extra_updaters,omitempty"`
}

func ValidCommandID(s string) bool {
	u, err := uuid.Parse(s)
	return err == nil && u != uuid.Nil && u.String() == s
}

func ValidateRequest(r Request) error {
	if (r.Version != ProtocolVersion && r.Version != RequestVersionExtra) || !ValidCommandID(r.CommandID) {
		return errors.New("invalid maintenance version or command UUID")
	}
	if r.Mode != "full" && r.Mode != "security" && r.Mode != "repair" {
		return errors.New("maintenance mode must be full, security or repair")
	}
	if r.Mode == "repair" && r.IncludeRebootClass {
		return errors.New("repair must preserve reboot-class holds")
	}
	return validateExtraList(r.Version, r.Mode, r.ExtraUpdaters, "request")
}

// validateExtraList enforces the v1/v2 split: a v1 object carries no updater
// list; a v2 object carries 1..8 unique implemented identifiers in registry
// order and never runs in repair mode (FR-026).
func validateExtraList(version int, mode string, ids []string, what string) error {
	if version == ProtocolVersion {
		if len(ids) != 0 {
			return fmt.Errorf("version 1 %s must not list extra updaters", what)
		}
		return nil
	}
	if mode == "repair" {
		return fmt.Errorf("repair %s must not list extra updaters", what)
	}
	if len(ids) == 0 || len(ids) > models.MaxExtraUpdaters {
		return fmt.Errorf("%s must list between 1 and %d extra updaters", what, models.MaxExtraUpdaters)
	}
	position := -1
	for _, id := range ids {
		next := -1
		for i, u := range extraupd.Registry() {
			if u.ID() == id {
				next = i
			}
		}
		if next < 0 || next <= position {
			return fmt.Errorf("%s lists an unknown, duplicate or unordered extra updater", what)
		}
		position = next
	}
	return nil
}

// ParseRequest requires explicit canonical fields, including false booleans;
// JSON null, duplicates, unknown fields and trailing values are rejected.
func ParseRequest(data []byte) (Request, error) {
	var r Request
	if len(data) == 0 || len(data) > MaxRequestBytes || !utf8.Valid(data) {
		return r, errors.New("invalid maintenance request size or encoding")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return r, errors.New("maintenance request must be one object")
	}
	seen := map[string]bool{}
	for d.More() {
		t, err = d.Token()
		if err != nil {
			return r, err
		}
		key, ok := t.(string)
		if !ok || seen[key] {
			return r, errors.New("duplicate maintenance field")
		}
		seen[key] = true
		var raw json.RawMessage
		if err = d.Decode(&raw); err != nil {
			return r, err
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return r, errors.New("null maintenance field")
		}
		switch key {
		case "version":
			err = json.Unmarshal(raw, &r.Version)
		case "command_id":
			err = json.Unmarshal(raw, &r.CommandID)
		case "mode":
			err = json.Unmarshal(raw, &r.Mode)
		case "include_reboot_class":
			err = json.Unmarshal(raw, &r.IncludeRebootClass)
		case "extra_updaters":
			err = json.Unmarshal(raw, &r.ExtraUpdaters)
		default:
			return r, fmt.Errorf("unknown maintenance field %q", key)
		}
		if err != nil {
			return r, err
		}
	}
	if _, err = d.Token(); err != nil {
		return r, err
	}
	if _, err = d.Token(); err != io.EOF {
		return r, errors.New("trailing maintenance JSON")
	}
	want := 4
	if r.Version == RequestVersionExtra {
		want = 5
	}
	if len(seen) != want || seen["extra_updaters"] != (want == 5) {
		return r, fmt.Errorf("maintenance version %d request requires exactly %d explicit fields", r.Version, want)
	}
	return r, ValidateRequest(r)
}

type HoldSnapshot struct {
	Packages []string          `json:"packages,omitempty"`
	Versions map[string]string `json:"versions,omitempty"`
}
type VersionChange struct {
	Package string `json:"package"`
	Before  string `json:"before"`
	After   string `json:"after"`
}
type Record struct {
	Version            int             `json:"version"`
	CommandID          string          `json:"command_id"`
	Mode               string          `json:"mode"`
	IncludeRebootClass bool            `json:"include_reboot_class"`
	StartedAt          time.Time       `json:"started_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	FinishedAt         *time.Time      `json:"finished_at,omitempty"`
	Phase              string          `json:"phase"`
	Outcome            string          `json:"outcome"`
	ExitCode           int             `json:"exit_code"`
	Interrupted        bool            `json:"interrupted"`
	Summary            string          `json:"summary,omitempty"`
	Output             string          `json:"output,omitempty"`
	OutputTruncated    bool            `json:"output_truncated"`
	Holds              HoldSnapshot    `json:"holds"`
	HoldsReleased      bool            `json:"holds_released"`
	HoldRestoreError   string          `json:"hold_restore_error,omitempty"`
	HoldCleanupFailed  bool            `json:"hold_cleanup_failed"`
	VersionChanges     []VersionChange `json:"version_changes,omitempty"`

	// Version 2 additions (EXTRA-UPD-001 section 5.3). All are empty, and so
	// absent from the JSON, on a version 1 record.
	RequestedExtraUpdaters []string        `json:"requested_extra_updaters,omitempty"`
	AptOutcome             string          `json:"apt_outcome,omitempty"`
	ExtraUpdaters          []UpdaterResult `json:"extra_updaters,omitempty"`
}

// UpdaterResult is the truthful outcome of one present updater in a v2 run
// (FR-037).
type UpdaterResult struct {
	ID                  string     `json:"id"`
	Outcome             string     `json:"outcome"`
	Reason              string     `json:"reason,omitempty"`
	ExitCode            *int       `json:"exit_code,omitempty"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
	VersionBefore       string     `json:"version_before,omitempty"`
	VersionAfter        string     `json:"version_after,omitempty"`
	SourceOwner         string     `json:"source_owner,omitempty"`
	SourceBranch        string     `json:"source_branch,omitempty"`
	RebootRequiredAfter bool       `json:"reboot_required_after,omitempty"`
	Note                string     `json:"note,omitempty"`
}

// Failing reports whether this result makes the command fail even when apt
// succeeded: the updater started and did not succeed (FR-012). Any skip is
// before the start, so it never fails a run; a launcher setup_failed or
// launcher_refused is recorded as failed, not skipped, and still fails it
// (DP-SEC-004).
func (u UpdaterResult) Failing() bool {
	switch u.Outcome {
	case UpdaterSuccess, UpdaterSkipped:
		return false
	default:
		return true
	}
}

// Refused reports a skip that is not neutral: the updater was pending and
// wanted, and rIOt refused to start it. It raises a warning event and a badge
// item without changing the outcome (FR-014).
func (u UpdaterResult) Refused() bool {
	return u.Outcome == UpdaterSkipped && !NeutralSkip(u.Reason) && u.Reason != SkipAptNotSuccessful
}

func (r Record) Terminal() bool { return r.Outcome != "running" && r.FinishedAt != nil }
func (r Record) Validate() error {
	if err := ValidateRequest(Request{Version: r.Version, CommandID: r.CommandID, Mode: r.Mode, IncludeRebootClass: r.IncludeRebootClass, ExtraUpdaters: r.RequestedExtraUpdaters}); err != nil {
		return err
	}
	if r.StartedAt.IsZero() || r.UpdatedAt.Before(r.StartedAt) || len(r.Output) > MaxOutputBytes || len(r.Summary) > 4096 || strings.TrimSpace(r.Phase) == "" {
		return errors.New("invalid maintenance record")
	}
	switch r.Outcome {
	case "running":
		if r.FinishedAt != nil {
			return errors.New("running record is finished")
		}
	case "success", "error", "interrupted":
		if r.FinishedAt == nil || r.FinishedAt.Before(r.StartedAt) {
			return errors.New("terminal record lacks valid finish time")
		}
	default:
		return errors.New("invalid maintenance outcome")
	}
	if r.Outcome == "success" && (r.Interrupted || r.ExitCode != 0 || r.HoldsReleased || r.HoldCleanupFailed || r.HoldRestoreError != "") {
		return errors.New("inconsistent success record")
	}
	return r.validateExtra()
}

// validateExtra checks the version 2 additions. A running entry exists only
// while the record itself is running and sits in that entry's own phase.
func (r Record) validateExtra() error {
	if r.Version == ProtocolVersion {
		if r.AptOutcome != "" || len(r.ExtraUpdaters) != 0 {
			return errors.New("version 1 record carries extra-updater fields")
		}
		return nil
	}
	switch r.AptOutcome {
	case "", aptOutcomeSuccess, aptOutcomeError, aptOutcomeInterrupt:
	default:
		return errors.New("invalid apt outcome")
	}
	if r.Outcome != "running" && r.AptOutcome == "" {
		return errors.New("terminal version 2 record lacks an apt outcome")
	}
	if len(r.ExtraUpdaters) > models.MaxExtraUpdaters {
		return errors.New("too many updater results")
	}
	seen := map[string]bool{}
	failing := false
	for _, u := range r.ExtraUpdaters {
		if !models.ExtraUpdaterIDPattern.MatchString(u.ID) || seen[u.ID] {
			return errors.New("invalid or duplicate updater result id")
		}
		seen[u.ID] = true
		if err := u.validate(r); err != nil {
			return err
		}
		failing = failing || u.Failing()
	}
	if r.Outcome == "success" && (r.AptOutcome != aptOutcomeSuccess || failing) {
		return errors.New("success record with a failed apt part or updater")
	}
	return nil
}

func (u UpdaterResult) validate(r Record) error {
	switch u.Outcome {
	case UpdaterRunning:
		if r.Outcome != "running" || r.Phase != phasePrefixExtraRun+u.ID {
			return errors.New("running updater outside its own phase")
		}
	case UpdaterSuccess, UpdaterFailed, UpdaterTimedOut, UpdaterInterrupted:
	case UpdaterSkipped:
		if !skipReasons[u.Reason] {
			return errors.New("invalid updater skip reason")
		}
	default:
		return errors.New("invalid updater outcome")
	}
	if u.Outcome != UpdaterSkipped && u.Reason != "" {
		return errors.New("updater reason only applies to a skip")
	}
	if len(u.Note) > maxUpdaterNote || len(u.VersionBefore) > maxUpdaterVersion || len(u.VersionAfter) > maxUpdaterVersion ||
		len(u.SourceOwner) > maxUpdaterSource || len(u.SourceBranch) > maxUpdaterSource {
		return errors.New("updater result field exceeds its bound")
	}
	return nil
}

func parseRecord(data []byte) (Record, error) {
	var r Record
	if len(data) > MaxRecordBytes || !utf8.Valid(data) {
		return r, errors.New("invalid record encoding or size")
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return r, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return r, err
	}
	if d.Decode(new(any)) != io.EOF {
		return r, errors.New("trailing record JSON")
	}
	return r, r.Validate()
}

// Root-owned files can still be corrupted by interrupted/manual operations.
// Reject ambiguous JSON at every nesting level rather than using last-key wins.
func rejectDuplicateKeys(data []byte) error {
	if !utf8.Valid(data) {
		return errors.New("invalid JSON UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return errors.New("JSON nesting exceeds maintenance limit")
		}
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				token, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok || seen[key] {
					return errors.New("duplicate JSON field")
				}
				seen[key] = true
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing maintenance JSON")
	}
	return nil
}

// Package maintenance defines the closed protocol shared by the unprivileged
// observer and the independent privileged maintenance service.
package maintenance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
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

var (
	ErrBusy        = errors.New("maintenance exclusion is busy")
	ErrUnsupported = errors.New("detached maintenance requires supported Linux apt/systemd installation")
	ErrHistoryFull = errors.New("maintenance history capacity reached; root operator must archive reviewed records")
)

type Request struct {
	Version            int    `json:"version"`
	CommandID          string `json:"command_id"`
	Mode               string `json:"mode"`
	IncludeRebootClass bool   `json:"include_reboot_class"`
}

func ValidCommandID(s string) bool {
	u, err := uuid.Parse(s)
	return err == nil && u != uuid.Nil && u.String() == s
}

func ValidateRequest(r Request) error {
	if r.Version != ProtocolVersion || !ValidCommandID(r.CommandID) {
		return errors.New("invalid maintenance version or command UUID")
	}
	if r.Mode != "full" && r.Mode != "security" && r.Mode != "repair" {
		return errors.New("maintenance mode must be full, security or repair")
	}
	if r.Mode == "repair" && r.IncludeRebootClass {
		return errors.New("repair must preserve reboot-class holds")
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
	if len(seen) != 4 {
		return r, errors.New("maintenance request requires four explicit fields")
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
}

func (r Record) Terminal() bool { return r.Outcome != "running" && r.FinishedAt != nil }
func (r Record) Validate() error {
	if err := ValidateRequest(Request{r.Version, r.CommandID, r.Mode, r.IncludeRebootClass}); err != nil {
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

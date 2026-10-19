package commands

import (
	"errors"
	"fmt"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Errors returned by ValidateReport and Service.HandleAgentReport for a report
// that is structurally unusable. Transports map both to HTTP 400 invalid_report
// (HTTP) or drop the message without a receipt (WS).
var (
	// ErrMalformedCommandID means a command ID is not a canonical lowercase UUID.
	// The raw value must never be logged (SEC C-1): log its length only.
	ErrMalformedCommandID = errors.New("malformed command id")
	// ErrInvalidReport covers an ID mismatch, an unknown kind, an unknown
	// progress state or a result report without a result.
	ErrInvalidReport = errors.New("invalid agent report")
)

// validateReportIDs checks the command ID of a report (SEC C-1): the path ID
// must be a canonical UUID, and a body ID, when present, must equal it. It runs
// before any repository access.
func validateReportIDs(pathID, bodyID string) error {
	if !models.IsCanonicalUUID(pathID) {
		return ErrMalformedCommandID
	}
	if bodyID != "" && bodyID != pathID {
		return fmt.Errorf("%w: body command_id does not match the path", ErrInvalidReport)
	}
	return nil
}

// ValidateReport checks the shape of a decoded report before it reaches the
// service: IDs (validateReportIDs), the kind, the progress state and the
// presence of a result. WS callers pass the report's own command ID as pathID.
func ValidateReport(pathID string, r models.CommandReport) error {
	if err := validateReportIDs(pathID, r.CommandID); err != nil {
		return err
	}
	switch r.Kind {
	case models.ReportKindAck:
	case models.ReportKindProgress:
		if r.State != string(models.CommandRunning) && r.State != string(models.CommandVerifying) {
			return fmt.Errorf("%w: progress state must be running or verifying", ErrInvalidReport)
		}
	case models.ReportKindResult:
		if r.Result == nil {
			return fmt.Errorf("%w: result report without a result", ErrInvalidReport)
		}
	default:
		return fmt.Errorf("%w: unknown report kind", ErrInvalidReport)
	}
	return nil
}

// sanitizeReport enforces the server-side bounds on every agent-supplied string
// (SEC C-1, C-2) and returns a copy; the caller's report is not modified.
//
//   - every string is made NUL-free and valid UTF-8 first, then truncated on a
//     rune boundary, so a content problem never becomes a database error;
//   - step must match ^[a-z0-9_]{1,64}$, otherwise it becomes "" and the
//     transition still applies;
//   - the progress message is cut to 1 KiB, result message to its last 64 KiB,
//     stderr to its last 8 KiB, output to 256 KiB (head and tail);
//   - reason must be in the documented enum, otherwise "".
func sanitizeReport(r models.CommandReport) models.CommandReport {
	out := r
	out.Step = models.SanitizeStep(r.Step)
	out.Message = models.TruncateHead(models.SanitizeText(r.Message), models.MaxProgressMessageBytes)
	if r.Result != nil {
		res := *r.Result
		res.Step = models.SanitizeStep(res.Step)
		res.Reason = models.SanitizeReason(res.Reason)
		res.Message = models.TruncateTail(models.SanitizeText(res.Message), models.MaxResultMsgBytes)
		res.Stderr = models.TruncateTail(models.SanitizeText(res.Stderr), models.MaxStderrTailBytes)
		res.Output = models.TruncateHeadTail(models.SanitizeText(res.Output), models.MaxReportOutputBytes)
		out.Result = &res
	}
	return out
}

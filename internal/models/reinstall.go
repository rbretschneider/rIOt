package models

import (
	"fmt"
	"time"
)

// EXPECT-008: installer re-run as expected activity. This file holds the bounds
// (AD-012), the step and failure-step tokens, the labels, the message renderers
// (AD-011) and the value types the repository, the commands service and the
// tracker share for a reinstall record. A record is a commands row with origin
// installer, created directly in running by the server when the installer's
// authenticated notice is accepted.

// Bounds of the reinstall path (AD-012). Each is a hard limit, not a default:
// the 10-minute bound is also a database CHECK (migration 000035).
const (
	// ExpectReinstallBound is the longest a reinstall window lasts, counted from
	// the server's receive time of the notice (FRD OQ-001).
	ExpectReinstallBound = 10 * time.Minute
	// ReinstallRateCap is the most records one device may create in any
	// ReinstallRateWindow (FR-010, DEC-1). It bounds the abuse budget (under 60
	// minutes of unalerted offline time per device per day), separately from
	// EXPECT-002's.
	ReinstallRateCap    = 6
	ReinstallRateWindow = 24 * time.Hour
	// ReinstallStuckAfter is how long after its bound a still-unresolved record
	// stays before it is made visible (NFR-004) and may be retired by the next
	// notice.
	ReinstallStuckAfter = time.Hour
	// ReinstallExecDeadlineS is exec_deadline_s of every record (the CHECK allows
	// at most 600).
	ReinstallExecDeadlineS = 600
)

// Step tokens of a reinstall record (all match StepPattern).
const (
	ReinstallStepWaitingForRestart = "waiting_for_restart"
	ReinstallStepConfirmed         = "confirmed"
)

// Failure-step tokens of a reinstall record (all match StepPattern).
const (
	ReinstallFailureUnconfirmedOnline  = "unconfirmed_online"
	ReinstallFailureUnconfirmedOffline = "unconfirmed_offline"
	ReinstallFailureUnresolved         = "unresolved"
)

// Labels (DEC-3). The activity label "Reinstalling agent" is web copy; the
// server owns the action label and the actor phrase.
const (
	ReinstallActionLabel = "Agent reinstall"
	ReinstallActorPhrase = "by installer"
)

// ReinstallResultMsgBytes bounds a server-rendered result_msg (as EXPECT-002).
const ReinstallResultMsgBytes = SelfUpdateResultMsgBytes

// ReinstallConfirmedResultMessage is the result_msg of a record closed by the
// device's registration.
const ReinstallConfirmedResultMessage = "confirmed: the agent registered again"

// ReinstallStaleResultMessage is the result_msg of a record retired as stale.
const ReinstallStaleResultMessage = "not resolved within 1h after the bound"

func reinstallLabelPhrase() string { return ReinstallActionLabel + " " + ReinstallActorPhrase }

// ReinstallNotBackMessage is FR-021: still offline at the bound.
func ReinstallNotBackMessage(hostname, id8 string) string {
	return fmt.Sprintf("%s still offline %s after %s (record %s)",
		selfUpdateHost(hostname), FormatDurationShort(ExpectReinstallBound), reinstallLabelPhrase(), id8)
}

// ReinstallNotConfirmedMessage is FR-022: not confirmed within the bound while
// the device is online.
func ReinstallNotConfirmedMessage(hostname string) string {
	return fmt.Sprintf("%s on %s was not confirmed within %s (agent did not restart)",
		reinstallLabelPhrase(), selfUpdateHost(hostname), FormatDurationShort(ExpectReinstallBound))
}

// ReinstallUnconfirmedOfflineMessage is the result_msg of a record whose device
// was offline at the bound.
func ReinstallUnconfirmedOfflineMessage(hostname string) string {
	return fmt.Sprintf("not confirmed: %s was offline %s after the start",
		selfUpdateHost(hostname), FormatDurationShort(ExpectReinstallBound))
}

// ReinstallRateCapMessage is FR-010.
func ReinstallRateCapMessage(hostname string) string {
	return fmt.Sprintf("Agent reinstall tracking paused on %s: more than %d reinstalls in %s; offline alerts during reinstalls are not suppressed",
		selfUpdateHost(hostname), ReinstallRateCap, FormatDurationShort(ReinstallRateWindow))
}

// ReinstallStuckMessage is the NFR-004 text for a record unresolved 1h after
// its bound.
func ReinstallStuckMessage(hostname, id8 string) string {
	return fmt.Sprintf("Agent reinstall tracking paused on %s: record %s (%s) could not be completed %s after its bound; the next reinstall replaces it",
		selfUpdateHost(hostname), id8, ReinstallActionLabel, FormatDurationShort(ReinstallStuckAfter))
}

// ---------------------------------------------------------------------------
// Value types
// ---------------------------------------------------------------------------

// ReinstallNotice is an authenticated notice the server received (server time
// At). Nothing from the request body is part of it (FR-012).
type ReinstallNotice struct {
	DeviceID, Hostname string
	At                 time.Time
}

// ReinstallDisposition is what the server did with a notice. It is also the
// token the installer reads from the 200 response.
type ReinstallDisposition string

const (
	// ReinstallCreated: a new record was inserted.
	ReinstallCreated ReinstallDisposition = "created"
	// ReinstallOpenRecord: the device already has an open record (FR-009).
	ReinstallOpenRecord ReinstallDisposition = "open_record"
	// ReinstallCapped: the rate cap refused the notice (FR-010).
	ReinstallCapped ReinstallDisposition = "capped"
	// ReinstallDeviceOffline: the device is offline on the server (FR-008 a).
	ReinstallDeviceOffline ReinstallDisposition = "device_offline"
	// ReinstallUnavailable: the store failed, the feature is not wired, or the
	// identity did not match.
	ReinstallUnavailable ReinstallDisposition = "unavailable"
)

// ReinstallCreateResult is the outcome of CreateReinstall.
type ReinstallCreateResult struct {
	Disposition ReinstallDisposition
	// Command is the created row (ReinstallCreated).
	Command *Command
	// Retired is a stale open row retired in the same transaction (NFR-004); the
	// caller broadcasts it.
	Retired *Command
	// CapEvent is the one rate-cap warning event written by this call, if any.
	CapEvent *Event
}

// ReinstallOutcome is how the tracker resolves a record at its bound.
type ReinstallOutcome string

const (
	// ReinstallUnconfirmedOnline ends it timed_out, the device online.
	ReinstallUnconfirmedOnline ReinstallOutcome = "unconfirmed_online"
	// ReinstallUnconfirmedOffline ends it timed_out, the device offline.
	ReinstallUnconfirmedOffline ReinstallOutcome = "unconfirmed_offline"
)

// State is the command state the outcome moves a record to.
func (o ReinstallOutcome) State() (CommandState, bool) {
	switch o {
	case ReinstallUnconfirmedOnline, ReinstallUnconfirmedOffline:
		return CommandTimedOut, true
	}
	return "", false
}

// FailureStep is the failure_step the outcome writes.
func (o ReinstallOutcome) FailureStep() string {
	switch o {
	case ReinstallUnconfirmedOnline:
		return ReinstallFailureUnconfirmedOnline
	case ReinstallUnconfirmedOffline:
		return ReinstallFailureUnconfirmedOffline
	}
	return ""
}

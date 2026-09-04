package oidc

// Browser-visible error codes (FRD §7.4). These four values are the entire
// vocabulary the browser ever sees; the specific reason is server-log only.
const (
	CodeFailed      = "sso_failed"
	CodeExpired     = "sso_expired"
	CodeDenied      = "sso_denied"
	CodeUnavailable = "sso_unavailable"
)

// Fixed, log-only reason vocabulary (ADD AD-010). QA asserts on these values;
// engineering must not invent additional ones without an ADD amendment.
const (
	ReasonDiscoveryFailed       = "discovery_failed"
	ReasonRandFailed            = "rand_failed"
	ReasonThrottled             = "throttled"
	ReasonNoTransaction         = "no_transaction"
	ReasonTransactionExpired    = "transaction_expired"
	ReasonIdPError              = "idp_error"
	ReasonStateMismatch         = "state_mismatch"
	ReasonMissingCode           = "missing_code"
	ReasonIdPUnreachable        = "idp_unreachable"
	ReasonTokenExchangeRejected = "token_exchange_rejected"
	ReasonMissingIDToken        = "missing_id_token"
	ReasonTokenInvalid          = "token_invalid"
	ReasonNonceMismatch         = "nonce_mismatch"
	ReasonClaimsIncomplete      = "claims_incomplete"
)

// LoginError is the sole error shape crossing the oidc package boundary. Code
// is browser-visible (one of the four Code* constants above); Reason is
// log-only (one of the Reason* constants above); Err is the wrapped cause and
// is never rendered to the browser.
type LoginError struct {
	Code   string
	Reason string
	Err    error
}

func (e *LoginError) Error() string {
	if e.Err != nil {
		return e.Reason + ": " + e.Err.Error()
	}
	return e.Reason
}

func (e *LoginError) Unwrap() error {
	return e.Err
}

// newLoginError constructs a *LoginError, wrapping cause (which may be nil).
func newLoginError(code, reason string, cause error) *LoginError {
	return &LoginError{Code: code, Reason: reason, Err: cause}
}

package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Messages for the two reboot-authorization rejections (PATCH-FLOW-001 §6).
const (
	msgRebootNotAllowed = "reboots are turned off on this agent (commands.allow_reboot: false)"
	msgRebootAuthUnsup  = "the agent must be updated to authorize a reboot with a patch"
)

const modeRepair = "repair"

// authRejection tells SendCommand how to refuse a manual os_update.
type authRejection struct {
	status int
	code   string
}

// applyManualOSUpdateAuth enforces FR-015..FR-018 on a manual os_update's params
// (mutating them). It returns nil to proceed, or the rejection to answer with.
// A non-nil error means the authorization could not be evaluated (server fault).
//
// This is the second place that may set include_reboot_class (the first is
// checkAutoPatch). It sets it only for an explicit, valid reboot_if_required=true
// that the host's own latest telemetry does not contradict. Telemetry can only
// narrow what is allowed: it never creates an authorization (NFR-003).
func (h *Handlers) applyManualOSUpdateAuth(ctx context.Context, deviceID string, params map[string]interface{}) (*authRejection, error) {
	if reservedParamVariants(params) {
		return &authRejection{http.StatusBadRequest, "invalid_request"}, nil
	}
	value, present := params[models.ParamRebootIfRequired]
	if !present {
		return nil, nil
	}
	authorized, isBool := value.(bool)
	if !isBool {
		return &authRejection{http.StatusBadRequest, "invalid_request"}, nil
	}
	if !authorized {
		delete(params, models.ParamRebootIfRequired)
		return nil, nil
	}
	if mode, _ := params["mode"].(string); mode == modeRepair {
		return &authRejection{http.StatusBadRequest, "invalid_request"}, nil
	}
	rejection, err := h.checkRebootCapability(ctx, deviceID)
	if rejection != nil || err != nil {
		return rejection, err
	}
	params["include_reboot_class"] = true
	return nil, nil
}

// reservedParamVariants strips every case variant of include_reboot_class and
// reports whether a case variant of reboot_if_required is present. encoding/json
// matches struct field names case-insensitively, so a future typed decode of
// os_update params must not find either key under another spelling (C-4).
func reservedParamVariants(params map[string]interface{}) (rejectVariant bool) {
	for key := range params {
		switch {
		case strings.EqualFold(key, models.ParamIncludeRebootClass):
			delete(params, key)
		case key != models.ParamRebootIfRequired && strings.EqualFold(key, models.ParamRebootIfRequired):
			rejectVariant = true
		}
	}
	return rejectVariant
}

// checkRebootCapability applies FR-018: the host's latest telemetry must report
// allow_reboot true and support for reboot authorization.
func (h *Handlers) checkRebootCapability(ctx context.Context, deviceID string) (*authRejection, error) {
	unsupported := &authRejection{http.StatusBadRequest, models.CodeRebootAuthUnsupported}
	if h.telemetry == nil {
		return unsupported, nil
	}
	snap, err := h.telemetry.GetLatestSnapshot(ctx, deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return unsupported, nil
	}
	if err != nil {
		slog.Error("os_update authorization: latest telemetry", "device_id", deviceID, "error", err)
		return nil, err
	}
	if snap == nil || snap.Data.Updates == nil {
		return unsupported, nil
	}
	updates := snap.Data.Updates
	if updates.AllowReboot != nil && !*updates.AllowReboot {
		return &authRejection{http.StatusBadRequest, models.CodeRebootNotAllowed}, nil
	}
	if updates.AllowReboot == nil || !bool(updates.SupportsRebootIfRequired) {
		return unsupported, nil
	}
	return nil, nil
}

// writeAuthRejection answers a refused manual os_update. invalid_request keeps
// the shared dockerError shape; the two authorization codes carry a sentence
// that names the reason.
func writeAuthRejection(w http.ResponseWriter, rejection *authRejection) {
	switch rejection.code {
	case models.CodeRebootNotAllowed:
		writeJSON(w, rejection.status, map[string]interface{}{"error": msgRebootNotAllowed, "code": rejection.code})
	case models.CodeRebootAuthUnsupported:
		writeJSON(w, rejection.status, map[string]interface{}{"error": msgRebootAuthUnsup, "code": rejection.code})
	default:
		dockerError(w, rejection.status, rejection.code)
	}
}

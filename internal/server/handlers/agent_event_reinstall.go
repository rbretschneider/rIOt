package handlers

import (
	"log/slog"
	"net/http"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
)

// reinstallNoticeResponse is the 200 body of an agent_reinstall_started notice
// (ADD section 6.1). The field order is fixed; the installer matches the token
// without depending on it.
type reinstallNoticeResponse struct {
	Status    string `json:"status"`
	Reinstall string `json:"reinstall"`
}

// receiveReinstallNotice is the handler side of the installer's reinstall notice
// (EXPECT-008 AD-002). The route's DeviceAuth has already rejected a missing key
// (401), an unknown or revoked key (401) and a key for another device (403), and
// MTLSDeviceAuth ran first when mTLS is on. Here the authenticated identity is
// checked once more (defence in depth): a request that did not pass DeviceAuth, or
// that names another device, is answered "unavailable" and records nothing.
//
// Nothing from the request body is read beyond its type (FR-012), and the notice
// never creates an events row on any disposition (NFR-005): the only event the
// path can store is the rate-cap warning the service writes. It always answers
// 200 once the body parsed, so the installer prints a truthful line.
func (h *Handlers) receiveReinstallNotice(w http.ResponseWriter, r *http.Request, deviceID string) {
	disposition := models.ReinstallUnavailable
	if authed := authenticatedDeviceID(r); authed != "" && authed == deviceID {
		hostname := ""
		if h.devices != nil {
			if d, err := h.devices.GetByID(r.Context(), deviceID); err == nil && d != nil {
				hostname = d.Hostname
			} else if err != nil {
				slog.Debug("reinstall notice: device read failed", "device_id", deviceID, "error", err.Error())
			}
		}
		res := h.cmdSvc().NoteReinstallNotice(r.Context(), commands.ReinstallNoticeInput{
			DeviceID: deviceID, Hostname: hostname, At: h.clock(),
		})
		disposition = res.Disposition
		if h.hub != nil {
			for i := range res.Events {
				h.hub.BroadcastEvent(&res.Events[i])
			}
		}
	}
	writeJSON(w, http.StatusOK, reinstallNoticeResponse{Status: "ok", Reinstall: string(disposition)})
}

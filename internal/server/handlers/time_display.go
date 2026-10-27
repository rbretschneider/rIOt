package handlers

import "net/http"

// timeDisplayResponse is the body of GET /api/v1/server/time-display.
type timeDisplayResponse struct {
	DisplayZone string `json:"display_zone"`
}

// TimeDisplay reports the server display zone name to the dashboard, which uses
// it for its Server time zone (TIME-12H-001 AD-006).
//
// GET /api/v1/server/time-display (admin session only)
// Responses:
//
//	200 OK — {"display_zone":"America/New_York"}, "UTC" when no configured zone
//	         was valid; Cache-Control: no-store
func (h *Handlers) TimeDisplay(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, timeDisplayResponse{DisplayZone: h.zone().String()})
}

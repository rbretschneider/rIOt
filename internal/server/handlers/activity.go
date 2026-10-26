package handlers

import (
	"net/http"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// ActivitySource is what the handlers need from the expected-activity tracker
// (*expect.Tracker, EXPECT-001 AD-014). It is an interface so a router without a
// tracker holds a nil interface, never a typed nil.
type ActivitySource interface {
	// Activities is the activity of every device that has one, from memory.
	Activities() models.ActivityResponse
	// NoteRegistration tells the tracker that a device registered again: it is
	// alive, so a powered-off record no longer applies (C-1 b).
	NoteRegistration(deviceID string, at time.Time)
}

// GetActivity handles GET /api/v1/activity: what each device is doing right now,
// as the server computes it from the commands and the device state (FR-034 to
// FR-043). It is the current state, not a delta: the dashboard refetches it after
// a WebSocket reconnect. Without a tracker the answer is 503, and the dashboard
// falls back to the plain status.
func (h *Handlers) GetActivity(w http.ResponseWriter, r *http.Request) {
	if h.activity == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "activity unavailable", "code": "activity_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, h.activity.Activities())
}

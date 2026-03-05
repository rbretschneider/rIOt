package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/go-chi/chi/v5"
)

// ListProbes handles GET /api/v1/probes.
func (h *Handlers) ListProbes(w http.ResponseWriter, r *http.Request) {
	probes, err := h.probeRepo.List(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list probes"}`, http.StatusInternalServerError)
		return
	}
	if probes == nil {
		probes = []models.Probe{}
	}

	// Attach latest result for each probe
	type probeWithResult struct {
		models.Probe
		LatestResult *models.ProbeResult `json:"latest_result,omitempty"`
	}
	results := make([]probeWithResult, len(probes))
	for i, p := range probes {
		results[i] = probeWithResult{Probe: p}
		if lr, err := h.probeRepo.LatestResult(r.Context(), p.ID); err == nil {
			results[i].LatestResult = lr
		}
	}
	writeJSON(w, http.StatusOK, results)
}

// CreateProbe handles POST /api/v1/probes.
func (h *Handlers) CreateProbe(w http.ResponseWriter, r *http.Request) {
	var probe models.Probe
	if err := json.NewDecoder(r.Body).Decode(&probe); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if probe.Name == "" || probe.Type == "" {
		http.Error(w, `{"error":"name and type are required"}`, http.StatusBadRequest)
		return
	}
	if probe.Config == nil {
		probe.Config = make(map[string]interface{})
	}
	if probe.IntervalSeconds == 0 {
		probe.IntervalSeconds = 60
	}
	if probe.TimeoutSeconds == 0 {
		probe.TimeoutSeconds = 10
	}
	if err := h.probeRepo.Create(r.Context(), &probe); err != nil {
		http.Error(w, `{"error":"failed to create probe"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, probe)
}

// UpdateProbe handles PUT /api/v1/probes/{id}.
func (h *Handlers) UpdateProbe(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	var probe models.Probe
	if err := json.NewDecoder(r.Body).Decode(&probe); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	probe.ID = id
	if probe.Config == nil {
		probe.Config = make(map[string]interface{})
	}
	if err := h.probeRepo.Update(r.Context(), &probe); err != nil {
		http.Error(w, `{"error":"failed to update probe"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, probe)
}

// DeleteProbe handles DELETE /api/v1/probes/{id}.
func (h *Handlers) DeleteProbe(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	if err := h.probeRepo.Delete(r.Context(), id); err != nil {
		http.Error(w, `{"error":"failed to delete probe"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// RunProbe handles POST /api/v1/probes/{id}/run.
func (h *Handlers) RunProbe(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	probe, err := h.probeRepo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"probe not found"}`, http.StatusNotFound)
		return
	}
	result := h.probeRunner.RunNow(r.Context(), *probe)
	writeJSON(w, http.StatusOK, result)
}

// GetProbeResults handles GET /api/v1/probes/{id}/results.
func (h *Handlers) GetProbeResults(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 100
	}
	results, err := h.probeRepo.ListResults(r.Context(), id, limit)
	if err != nil {
		http.Error(w, `{"error":"failed to list results"}`, http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []models.ProbeResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

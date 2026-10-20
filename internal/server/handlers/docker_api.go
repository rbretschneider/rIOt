package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/redact"
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/go-chi/chi/v5"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var dockerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func dockerError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]interface{}{"error": strings.ReplaceAll(code, "_", " "), "code": code})
}
func dockerDecode(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		dockerError(w, 415, "invalid_request")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		dockerError(w, 400, "invalid_request")
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		dockerError(w, 400, "invalid_request")
		return false
	}
	return true
}
func (h *Handlers) dockerSnapshot(r *http.Request, device string) (*models.TelemetrySnapshot, bool) {
	if h.telemetry == nil {
		return nil, false
	}
	s, e := h.telemetry.GetLatestSnapshot(r.Context(), device)
	return s, e == nil && s != nil && s.Data.Docker != nil
}
func dockerSelf(cs []models.ContainerInfo) map[string]string {
	out := map[string]string{}
	projects := map[string]bool{}
	for _, c := range cs {
		if strings.Contains(strings.ToLower(c.Image), "riot-server") {
			out[c.Name] = "server"
			projects[c.Labels[models.LabelComposeProject]] = true
		}
		if c.Riot != nil && (c.Riot.Role == "server" || c.Riot.Role == "database") {
			out[c.Name] = c.Riot.Role
		}
	}
	for _, c := range cs {
		if p := c.Labels[models.LabelComposeProject]; p != "" && projects[p] && out[c.Name] == "" {
			out[c.Name] = "database"
		}
	}
	return out
}
func (h *Handlers) DockerHosts(w http.ResponseWriter, r *http.Request) {
	ds, e := h.devices.List(r.Context())
	if e != nil {
		dockerError(w, 500, "internal")
		return
	}
	projections := map[string]db.DockerHostProjection{}
	projector, projected := h.telemetry.(interface {
		GetDockerHostProjection(context.Context) ([]db.DockerHostProjection, error)
	})
	if projected {
		rows, err := projector.GetDockerHostProjection(r.Context())
		if err != nil {
			dockerError(w, 500, "internal")
			return
		}
		for _, row := range rows {
			projections[row.DeviceID] = row
		}
	}
	out := []map[string]interface{}{}
	for _, d := range ds {
		row := map[string]interface{}{"device_id": d.ID, "hostname": d.Hostname, "status": d.Status, "docker_available": d.DockerAvailable, "capabilities": []string{}, "inventory_enabled": false, "container_logs": "unknown"}
		if d.PrimaryIP != "" {
			row["primary_ip"] = d.PrimaryIP
		}
		lastSeen := d.LastHeartbeat
		if d.LastTelemetry != nil && (lastSeen == nil || d.LastTelemetry.After(*lastSeen)) {
			lastSeen = d.LastTelemetry
		}
		if lastSeen != nil {
			row["last_seen"] = lastSeen
		}
		if projected {
			p := projections[d.ID]
			if p.Capabilities == nil {
				p.Capabilities = []string{}
			}
			row["docker_available"] = p.Available
			row["capabilities"] = p.Capabilities
			row["inventory_enabled"] = p.InventoryEnabled
			if p.ContainerLogsEnabled != nil {
				if *p.ContainerLogsEnabled {
					row["container_logs"] = "enabled"
				} else {
					row["container_logs"] = "disabled"
				}
			}
			if p.SelfRole != "" {
				row["self_role"] = p.SelfRole
			}
		} else if s, ok := h.dockerSnapshot(r, d.ID); ok {
			row["capabilities"] = s.Data.Docker.Capabilities
			row["inventory_enabled"] = s.Data.Docker.InventoryEnabled
			if s.Data.Docker.ContainerLogsEnabled != nil {
				if *s.Data.Docker.ContainerLogsEnabled {
					row["container_logs"] = "enabled"
				} else {
					row["container_logs"] = "disabled"
				}
			}
			for _, role := range dockerSelf(s.Data.Docker.Containers) {
				row["self_role"] = role
				break
			}
		}
		for k, v := range h.dockerLogHostState(d.ID) {
			row[k] = v
		}
		out = append(out, row)
	}
	writeJSON(w, 200, out)
}
func (h *Handlers) DockerBulkActions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action  string `json:"action"`
		Targets []struct {
			DeviceID  string `json:"device_id"`
			Container string `json:"container"`
		} `json:"targets"`
		ConfirmCount int    `json:"confirm_count"`
		ConfirmHost  string `json:"confirm_host"`
	}
	if !dockerDecode(w, r, &req) {
		return
	}
	if len(req.Targets) == 0 || len(req.Targets) > 100 {
		dockerError(w, 400, "too_many_targets")
		return
	}
	if req.Action != "start" && req.Action != "stop" && req.Action != "restart" && req.Action != "update" {
		dockerError(w, 400, "invalid_request")
		return
	}
	type target struct {
		device, name, id, host  string
		applicable, found, self bool
	}
	ts := []target{}
	hosts := map[string]bool{}
	count := 0
	seen := map[string]bool{}
	for _, t := range req.Targets {
		if !models.IsCanonicalUUID(t.DeviceID) || !dockerName.MatchString(t.Container) {
			dockerError(w, 400, "invalid_identifier")
			return
		}
		key := t.DeviceID + "/" + t.Container
		if seen[key] {
			continue
		}
		seen[key] = true
		v := target{device: t.DeviceID, name: t.Container}
		if d, e := h.devices.GetByID(r.Context(), t.DeviceID); e == nil {
			v.host = d.Hostname
		}
		if s, ok := h.dockerSnapshot(r, t.DeviceID); ok {
			for _, c := range s.Data.Docker.Containers {
				if strings.TrimLeft(c.Name, "/") == t.Container {
					v.found = true
					v.id = c.ID
					v.applicable = commands.Applicable(req.Action, c.State)
					v.self = dockerSelf(s.Data.Docker.Containers)[c.Name] != ""
					break
				}
			}
		}
		if v.applicable {
			count++
			hosts[v.device] = true
			if req.Action == "stop" && v.self && req.ConfirmHost != v.host {
				dockerError(w, 400, "confirmation_required")
				return
			}
		}
		ts = append(ts, v)
	}
	if req.Action == "stop" && (count > 5 || len(hosts) > 1) && req.ConfirmCount != count {
		dockerError(w, 400, "confirmation_required")
		return
	}
	results := []map[string]interface{}{}
	updates := map[string][]target{}
	for _, t := range ts {
		row := map[string]interface{}{"device_id": t.device, "container": t.name, "outcome": "skipped", "reason": "not_applicable"}
		if !t.found {
			row["reason"] = "not_found"
		} else if t.applicable {
			if req.Action == "update" {
				updates[t.device] = append(updates[t.device], t)
				continue
			}
			res, e := h.cmdSvc().Dispatch(r.Context(), dockerDispatchRequest(r, t.device, "docker_"+req.Action, map[string]interface{}{"container": t.name, "container_id": t.id}))
			delete(row, "reason")
			if e != nil {
				row["outcome"] = "error"
				row["reason"] = "internal"
			} else if res.Conflict != nil {
				row["outcome"] = "conflict"
				row["existing_command_id"] = res.Conflict.ID
			} else {
				row["outcome"] = "created"
				row["command_id"] = res.Command.ID
			}
		}
		results = append(results, row)
	}
	for device, targets := range updates {
		ids := []string{}
		for _, t := range targets {
			ids = append(ids, t.id)
		}
		res, e := h.cmdSvc().DispatchBulkDocker(r.Context(), device, ids, dockerDispatchRequest(r, device, "docker_bulk_update", nil))
		for _, t := range targets {
			row := map[string]interface{}{"device_id": device, "container": t.name, "outcome": "error", "reason": "internal"}
			if e == nil {
				if res.Command != nil {
					row["outcome"] = "created"
					row["command_id"] = res.Command.ID
					delete(row, "reason")
				}
				for _, c := range res.Conflicts {
					if c.ContainerID == t.id {
						row["outcome"] = "conflict"
						row["existing_command_id"] = c.ExistingCommandID
						delete(row, "command_id")
						delete(row, "reason")
					}
				}
			}
			results = append(results, row)
		}
	}
	writeJSON(w, 200, map[string]interface{}{"results": results})
}
func (h *Handlers) DockerHistory(w http.ResponseWriter, r *http.Request) {
	device := chi.URLParam(r, "id")
	name := chi.URLParam(r, "name")
	project := chi.URLParam(r, "project")
	if !models.IsCanonicalUUID(device) || (!dockerName.MatchString(name) && !dockerName.MatchString(project)) {
		dockerError(w, 400, "invalid_identifier")
		return
	}
	s, ok := h.dockerSnapshot(r, device)
	if !ok {
		dockerError(w, 404, "container_not_found")
		return
	}
	names := []string{}
	for _, c := range s.Data.Docker.Containers {
		n := strings.TrimLeft(c.Name, "/")
		if n == name || (project != "" && c.Labels[models.LabelComposeProject] == project) {
			names = append(names, n)
		}
	}
	if len(names) == 0 && project != "" && h.dockerRepo != nil {
		if row, err := h.dockerRepo.Stack(r.Context(), device, project); err == nil && row["forgotten"] != true {
			names = dockerStrings(row["member_names"])
			if row["members_truncated"] == true && r.URL.Query().Get("services") == "" {
				dockerError(w, 400, "too_many_targets")
				return
			}
		}
	}
	if len(names) == 0 {
		dockerError(w, 404, "container_not_found")
		return
	}
	if project != "" && r.URL.Query().Get("services") != "" {
		want := strings.Split(r.URL.Query().Get("services"), ",")
		filtered := []string{}
		for _, n := range names {
			for _, v := range want {
				if n == v {
					filtered = append(filtered, n)
				}
			}
		}
		names = filtered
	}
	if len(names) > 25 {
		dockerError(w, 400, "too_many_targets")
		return
	}
	now := h.clock()
	days, limitedBy := h.effectiveLogRetention()
	oldest := now.Add(-time.Duration(days) * 24 * time.Hour)
	from, to := now.Add(-time.Hour), now
	limit := 5000
	q := r.URL.Query()
	for _, k := range []string{"from", "to", "after"} {
		p := &from
		if k == "to" {
			p = &to
		}
		if v := q.Get(k); v != "" {
			t, e := time.Parse(time.RFC3339Nano, v)
			if e != nil {
				dockerError(w, 400, "invalid_request")
				return
			}
			*p = t
		}
	}
	if q.Get("limit") != "" {
		n, e := strconv.Atoi(q.Get("limit"))
		if e != nil || n < 1 || n > 5000 {
			dockerError(w, 400, "invalid_request")
			return
		}
		limit = n
	}
	if from.After(to) {
		dockerError(w, 400, "invalid_request")
		return
	}
	clamped := from.Before(oldest)
	if clamped {
		from = oldest
	}
	if to.After(now.Add(5 * time.Minute)) {
		to = now.Add(5 * time.Minute)
	}
	var before time.Time
	var bid int64
	if v := q.Get("before"); v != "" {
		raw, e := base64.RawURLEncoding.DecodeString(v)
		parts := strings.Split(string(raw), "|")
		if e != nil || len(parts) != 2 {
			dockerError(w, 400, "invalid_request")
			return
		}
		before, e = time.Parse(time.RFC3339Nano, parts[0])
		if e != nil {
			dockerError(w, 400, "invalid_request")
			return
		}
		bid, e = strconv.ParseInt(parts[1], 10, 64)
		if e != nil {
			dockerError(w, 400, "invalid_request")
			return
		}
	}
	if h.dockerRepo == nil {
		dockerError(w, 503, "unavailable")
		return
	}
	rows, e := h.dockerRepo.Logs(r.Context(), device, names, from, to, before, bid, q.Get("after") != "", limit+1)
	if e != nil {
		dockerError(w, 500, "internal")
		return
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	entries := []map[string]interface{}{}
	var newest time.Time
	for _, v := range rows {
		entry := map[string]interface{}{"t": v.Timestamp, "s": "o"}
		if v.Stream == "stderr" {
			entry["s"] = "e"
		}
		line, _ := redact.MaskLine(v.Line)
		entry["l"] = line
		if v.AgentTS != nil {
			entry["agent_t"] = v.AgentTS
		}
		if project != "" {
			entry["container"] = v.ContainerName
		}
		if v.Timestamp.After(newest) {
			newest = v.Timestamp
		}
		if v.Stream == "gap_agent" || v.Stream == "gap_server" || v.Stream == "gap_quota" {
			n, err := strconv.Atoi(strings.TrimSuffix(v.Line, "+"))
			if err == nil && n > 0 && n <= 10000000 {
				reason := map[string]string{"gap_agent": "agent_cap", "gap_server": "server_cap", "gap_quota": "quota_sampled"}[v.Stream]
				delete(entry, "l")
				delete(entry, "s")
				entry["gap"] = map[string]interface{}{"count": n, "lower_bound": strings.HasSuffix(v.Line, "+"), "reason": reason}
			}
		}
		entries = append(entries, entry)
	}
	if q.Get("after") == "" {
		for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
			entries[i], entries[j] = entries[j], entries[i]
		}
	}
	out := map[string]interface{}{"entries": entries, "has_more": more, "retention_days": days, "oldest_retained_at": oldest, "range_clamped": clamped, "retention_limited_by": limitedBy, "pre_recreate_history": true, "collector": "unknown", "server_now": now}
	if !newest.IsZero() {
		out["newest_stored_at"] = newest
	}
	if more && len(rows) > 0 {
		v := rows[len(rows)-1]
		out["next_before"] = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s|%d", v.Timestamp.Format(time.RFC3339Nano), v.ID)))
	}
	if s.Data.Docker.ContainerLogsEnabled == nil {
		if exists, err := h.dockerRepo.HasLogData(r.Context(), device, now); err == nil && !exists {
			out["collector"] = "unknown_no_data"
		}
	}
	if s.Data.Docker.ContainerLogsEnabled != nil {
		if *s.Data.Docker.ContainerLogsEnabled {
			out["collector"] = "enabled"
		} else {
			out["collector"] = "disabled"
		}
	}
	writeJSON(w, 200, out)
}

func (h *Handlers) DockerIdentifiers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, key := range []string{"id", "deviceId"} {
			if v := chi.URLParam(r, key); v != "" && !models.IsCanonicalUUID(v) {
				dockerError(w, 400, "invalid_identifier")
				return
			}
		}
		for _, key := range []string{"name", "project"} {
			if v := chi.URLParam(r, key); v != "" && !dockerName.MatchString(v) {
				dockerError(w, 400, "invalid_identifier")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func dockerDispatchError(w http.ResponseWriter, err error) {
	if errors.Is(err, commands.ErrDeviceNotFound) {
		dockerError(w, 404, "device_not_found")
		return
	}
	if errors.Is(err, commands.ErrCreationRateLimited) {
		dockerError(w, 429, "rate_limited")
		return
	}
	dockerError(w, 500, "internal")
}

package handlers

import (
	"encoding/base64"
	"encoding/json"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/redact"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/go-chi/chi/v5"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func dockerCommandEntry(c models.Command, hostname string) map[string]interface{} {
	message, _ := redact.MaskLine(models.TruncateHead(redact.StripANSI(c.ResultMsg), 2048))
	return map[string]interface{}{"kind": "command", "id": c.ID, "device_id": c.DeviceID, "hostname": hostname, "container": c.Params["container"], "stack": c.Params["compose_project"], "action": c.Action, "actor_kind": c.ActorKind, "actor_auth": c.ActorAuth, "actor_name": c.ActorName, "actor_sub": c.ActorSub, "status": c.Status, "step": c.Step, "failure_step": c.FailureStep, "message": message, "requested_at": c.CreatedAt, "started_at": c.StartedAt, "finished_at": c.FinishedAt, "duration_ms": c.DurationMs, "target_exists": true}
}
func (h *Handlers) DockerActivity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 25
	if q.Get("limit") != "" {
		n, e := strconv.Atoi(q.Get("limit"))
		if e != nil || n < 1 || n > 100 {
			dockerError(w, 400, "invalid_filter")
			return
		}
		limit = n
	}
	f := db.DockerActivityFilter{Device: q.Get("device_id"), Container: q.Get("container"), Stack: q.Get("stack"), Actor: q.Get("actor"), Kind: q.Get("type"), From: h.clock().Add(-90 * 24 * time.Hour), To: h.clock(), Limit: limit + 1}
	if (f.Container != "" || f.Stack != "") && f.Device == "" {
		dockerError(w, 400, "invalid_filter")
		return
	}
	if f.Device != "" && !models.IsCanonicalUUID(f.Device) {
		dockerError(w, 400, "invalid_filter")
		return
	}
	if f.Kind != "" && f.Kind != "command" && f.Kind != "event" {
		dockerError(w, 400, "invalid_filter")
		return
	}
	if len(f.Actor) > 512 {
		dockerError(w, 400, "invalid_filter")
		return
	}
	if f.Actor != "" && !strings.HasPrefix(f.Actor, "sub:") {
		for _, a := range strings.Split(f.Actor, ",") {
			if a != "operator" && a != "system" && a != "agent" && a != "unknown" {
				dockerError(w, 400, "invalid_filter")
				return
			}
		}
	}
	states, ok := models.ParseStatusFilter(q.Get("state"))
	if !ok {
		dockerError(w, 400, "invalid_filter")
		return
	}
	f.States = states
	for k, p := range map[string]*time.Time{"from": &f.From, "to": &f.To} {
		if v := q.Get(k); v != "" {
			t, e := time.Parse(time.RFC3339Nano, v)
			if e != nil {
				dockerError(w, 400, "invalid_filter")
				return
			}
			*p = t
		}
	}
	if f.From.After(f.To) {
		dockerError(w, 400, "invalid_filter")
		return
	}
	if cursor := q.Get("cursor"); cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			dockerError(w, 400, "invalid_filter")
			return
		}
		var cursor struct {
			T    time.Time `json:"t"`
			Kind string    `json:"kind"`
			ID   string    `json:"id"`
		}
		if len(raw) > 512 || json.Unmarshal(raw, &cursor) != nil || cursor.T.IsZero() || (cursor.Kind != "command" && cursor.Kind != "event") || len(cursor.ID) > 64 {
			dockerError(w, 400, "invalid_filter")
			return
		}
		f.Before = cursor.T
		f.BeforeKind = cursor.Kind
		f.BeforeID = cursor.ID
	}
	if h.dockerRepo == nil {
		writeJSON(w, 200, map[string]interface{}{"entries": []interface{}{}})
		return
	}
	if f.Container != "" {
		f.TargetKeys = []string{models.TargetKeyContainer(f.Container)}
		if snap, ok := h.dockerSnapshot(r, f.Device); ok {
			for _, c := range snap.Data.Docker.Containers {
				if c.Name == f.Container {
					if wd := c.Labels[models.LabelComposeWorkingDir]; wd != "" {
						f.TargetKeys = append(f.TargetKeys, models.TargetKeyCompose(wd))
					}
				}
			}
		}
	}
	if f.Stack != "" {
		if snap, ok := h.dockerSnapshot(r, f.Device); ok {
			for _, c := range snap.Data.Docker.Containers {
				if c.Labels[models.LabelComposeProject] == f.Stack {
					f.Members = append(f.Members, c.Name)
				}
			}
		}
		if len(f.Members) == 0 {
			f.Members = []string{"!missing-stack!"}
		}
	}
	hosts := map[string]string{}
	if ds, e := h.devices.List(r.Context()); e == nil {
		for _, d := range ds {
			hosts[d.ID] = d.Hostname
		}
	}
	entries := []map[string]interface{}{}
	if f.Kind != "event" {
		cs, e := h.dockerRepo.ActivityCommands(r.Context(), f)
		if e != nil {
			dockerError(w, 500, "internal")
			return
		}
		for _, c := range cs {
			entry := dockerCommandEntry(c, hosts[c.DeviceID])
			entry["target_exists"] = h.dockerTargetExists(r, c)
			entries = append(entries, entry)
		}
	}
	if f.Kind != "command" && len(f.States) == 0 && (f.Actor == "" || f.Actor == "agent") {
		es, e := h.dockerRepo.ActivityEvents(r.Context(), f)
		if e != nil {
			dockerError(w, 500, "internal")
			return
		}
		for _, ev := range es {
			msg, _ := redact.MaskLine(models.TruncateHead(redact.StripANSI(ev.Message), 2048))
			entries = append(entries, map[string]interface{}{"kind": "event", "id": strconv.FormatInt(ev.ID, 10), "device_id": ev.DeviceID, "hostname": hosts[ev.DeviceID], "container": ev.ContainerName, "action": ev.Type, "actor_kind": "agent_observed", "actor_auth": "", "actor_name": "", "actor_sub": "", "attribution": "agent", "message": msg, "requested_at": ev.CreatedAt, "target_exists": true})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i]["requested_at"].(time.Time), entries[j]["requested_at"].(time.Time)
		if !a.Equal(b) {
			return a.After(b)
		}
		ak, bk := entries[i]["kind"].(string), entries[j]["kind"].(string)
		if ak != bk {
			return ak < bk
		}
		return entries[i]["id"].(string) > entries[j]["id"].(string)
	})
	out := map[string]interface{}{}
	if len(entries) > limit {
		entries = entries[:limit]
		last := entries[len(entries)-1]
		raw, _ := json.Marshal(map[string]interface{}{"t": last["requested_at"], "kind": last["kind"], "id": last["id"]})
		out["next_cursor"] = base64.RawURLEncoding.EncodeToString(raw)
	}
	out["entries"] = entries
	writeJSON(w, 200, out)
}
func (h *Handlers) DockerActivityDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "commandId")
	device := r.URL.Query().Get("device_id")
	if !models.IsCanonicalUUID(id) || !models.IsCanonicalUUID(device) {
		dockerError(w, 404, "command_not_found")
		return
	}
	c, e := h.commandRepo.GetForDevice(r.Context(), id, device)
	if e != nil || c == nil || !strings.HasPrefix(c.Action, "docker_") {
		dockerError(w, 404, "command_not_found")
		return
	}
	host := ""
	if d, e := h.devices.GetByID(r.Context(), device); e == nil {
		host = d.Hostname
	}
	steps := append([]models.DockerCommandStep{}, c.Steps...)
	for i := range steps {
		steps[i].Output, _ = redact.MaskText(steps[i].Output, "<masked>")
	}
	results := append([]models.DockerTargetResult{}, c.TargetResults...)
	for i := range results {
		results[i].Message, _ = redact.MaskLine(results[i].Message)
	}
	events := []map[string]interface{}{}
	if h.dockerRepo != nil {
		if linked, err := h.dockerRepo.CommandEvents(r.Context(), device, id); err == nil {
			for _, ev := range linked {
				message, _ := redact.MaskLine(models.TruncateHead(redact.StripANSI(ev.Message), 2048))
				events = append(events, map[string]interface{}{"type": ev.Type, "created_at": ev.CreatedAt, "message": message, "attribution": ev.CommandAttr})
			}
		}
	}
	truncated := false
	for _, step := range steps {
		truncated = truncated || step.Truncated
	}
	entry := dockerCommandEntry(*c, host)
	entry["target_exists"] = h.dockerTargetExists(r, *c)
	if !c.State().IsTerminal() {
		results = []models.DockerTargetResult{}
	}
	writeJSON(w, 200, map[string]interface{}{"entry": entry, "steps": steps, "output_truncated": truncated, "target_results": results, "events": events})
}

func (h *Handlers) dockerTargetExists(r *http.Request, c models.Command) bool {
	if h.devices == nil {
		return false
	}
	if d, e := h.devices.GetByID(r.Context(), c.DeviceID); e != nil || d == nil {
		return false
	}
	name, _ := c.Params["container"].(string)
	project, _ := c.Params["compose_project"].(string)
	if name == "" && project == "" {
		return true
	}
	snap, ok := h.dockerSnapshot(r, c.DeviceID)
	if !ok {
		return false
	}
	for _, ctr := range snap.Data.Docker.Containers {
		if name != "" && strings.TrimLeft(ctr.Name, "/") == name {
			return true
		}
		if project != "" && ctr.Labels[models.LabelComposeProject] == project {
			return true
		}
	}
	return false
}

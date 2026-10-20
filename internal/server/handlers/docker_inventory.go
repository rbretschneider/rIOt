package handlers

import (
	"encoding/json"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strconv"
	"time"
)

func (h *Handlers) dockerInventory(r *http.Request, device string) *models.DockerInventory {
	if h.dockerRepo != nil {
		if v, e := h.dockerRepo.Inventory(r.Context(), device); e == nil {
			return v
		}
	}
	if s, ok := h.dockerSnapshot(r, device); ok {
		return s.Data.DockerInventory
	}
	return nil
}
func (h *Handlers) DockerInventory(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "images" && kind != "volumes" && kind != "networks" {
		dockerError(w, 400, "invalid_filter")
		return
	}
	ds, e := h.devices.List(r.Context())
	if e != nil {
		dockerError(w, 500, "internal")
		return
	}
	hosts := []map[string]interface{}{}
	items := []map[string]interface{}{}
	for _, d := range ds {
		host := map[string]interface{}{"device_id": d.ID, "hostname": d.Hostname, "state": "never_collected", "stale": false, "truncated": false}
		snap, ok := h.dockerSnapshot(r, d.ID)
		if !ok {
			host["state"] = "unsupported"
		} else if !snap.Data.Docker.InventoryEnabled {
			host["state"] = "not_enabled"
		}
		v := h.dockerInventory(r, d.ID)
		if v != nil {
			host["state"] = "ok"
			host["collected_at"] = v.CollectedAt
			host["stale"] = h.clock().Sub(v.CollectedAt) > 15*time.Minute
			host["truncated"] = v.Truncated
			host["disk_usage"] = v.DiskUsage
			host["sizes_available"] = v.SizesAvailable
			var rows interface{}
			switch kind {
			case "images":
				rows = v.Images
			case "volumes":
				rows = v.Volumes
			case "networks":
				rows = v.Networks
			}
			raw, _ := json.Marshal(rows)
			var list []map[string]interface{}
			json.Unmarshal(raw, &list)
			for _, item := range list {
				item["device_id"] = d.ID
				items = append(items, item)
			}
		}
		hosts = append(hosts, host)
	}
	writeJSON(w, 200, map[string]interface{}{"hosts": hosts, "items": items})
}
func (h *Handlers) DockerInventoryRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !dockerDecode(w, r, &req) {
		return
	}
	device := chi.URLParam(r, "id")
	if !h.dockerCap(w, r, device, models.DockerCapInventory) {
		return
	}
	res, e := h.cmdSvc().Dispatch(r.Context(), dockerDispatchRequest(r, device, "docker_inventory_refresh", nil))
	if e != nil {
		dockerDispatchError(w,e)
		return
	}
	if res.Conflict != nil {
		writeCommandConflict(w, res.Conflict, res.ConflictKey)
		return
	}
	writeJSON(w, 201, res.Command)
}
func (h *Handlers) DockerPrunePreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind     string   `json:"kind"`
		ItemKind string   `json:"item_kind"`
		Items    []string `json:"items"`
	}
	if !dockerDecode(w, r, &req) {
		return
	}
	device := chi.URLParam(r, "id")
	if !h.dockerCap(w, r, device, models.DockerCapPrune) {
		return
	}
	if req.Kind != "images" && req.Kind != "images_all" && req.Kind != "volumes" && req.Kind != "networks" && req.Kind != "selected" {
		dockerError(w, 400, "invalid_request")
		return
	}
	v := h.dockerInventory(r, device)
	if v == nil {
		dockerError(w, 409, "inventory_stale")
		return
	}
	kind := req.ItemKind
	if req.Kind != "selected" {
		kind = map[string]string{"images": "image", "images_all": "image", "volumes": "volume", "networks": "network"}[req.Kind]
	}
	if kind != "image" && kind != "volume" && kind != "network" {
		dockerError(w, 400, "invalid_request")
		return
	}
	selected := map[string]bool{}
	if len(req.Items) > 2000 {
		dockerError(w, 400, "too_many_targets")
		return
	}
	for _, id := range req.Items {
		selected[id] = true
	}
	list := []map[string]interface{}{}
	items := map[string]map[string]interface{}{}
	bytes := int64(0)
	add := func(id string, item map[string]interface{}) {
		if req.Kind == "selected" && !selected[id] {
			return
		}
		if len(items) >= 2000 {
			return
		}
		items[id] = item
		list = append(list, item)
		if n, ok := item["size"].(int64); ok {
			bytes += n
		}
	}
	switch kind {
	case "image":
		for _, i := range v.Images {
			if i.InUse || i.Containers > 0 || (req.Kind == "images" && len(i.Tags) > 0) {
				continue
			}
			add(i.ID, map[string]interface{}{"id": i.ID, "name": i.ID, "tags": i.Tags, "size": i.Size, "selected_default": true})
		}
	case "volume":
		for _, i := range v.Volumes {
			if i.InUse {
				continue
			}
			add(i.Name, map[string]interface{}{"id": i.Name, "name": i.Name, "created_at": i.CreatedAt, "size": i.Size, "selected_default": i.Anonymous})
		}
	case "network":
		for _, i := range v.Networks {
			if i.InUse || i.Builtin || i.Name == "host" || i.Name == "none" || i.Name == "bridge" || i.Name == "ingress" {
				continue
			}
			add(i.ID, map[string]interface{}{"id": i.ID, "name": i.Name, "selected_default": true})
		}
	}
	id := dockerRandom()
	_, actor, _ := dockerActor(r)
	rt := h.dockerState()
	rt.mu.Lock()
	for key, p := range rt.previews {
		if !p.expires.After(h.clock()) {
			delete(rt.previews, key)
		}
	}
	if len(rt.previews) >= 50 {
		var oldest string
		var expiry time.Time
		for k, p := range rt.previews {
			if oldest == "" || p.expires.Before(expiry) {
				oldest = k
				expiry = p.expires
			}
		}
		delete(rt.previews, oldest)
	}
	rt.previews[id] = dockerPreview{device: device, actor: actor, kind: kind, expires: h.clock().Add(10 * time.Minute), items: items}
	rt.mu.Unlock()
	confirmation := map[string]interface{}{"kind": "none"}
	if kind == "volume" {
		confirmation["kind"] = "host"
	} else if len(items) > 20 {
		confirmation["kind"] = "count"
		confirmation["count"] = len(items)
	}
	age := h.clock().Sub(v.CollectedAt).Seconds()
	writeJSON(w, 200, map[string]interface{}{"preview_id": id, "item_kind": kind, "items": list, "count": len(items), "reclaimable_bytes": bytes, "inventory_age_s": age, "requires_refresh": age > 900, "confirmation": confirmation})
}
func (h *Handlers) DockerPruneExecute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PreviewID string   `json:"preview_id"`
		Selected  []string `json:"selected"`
		Confirm   string   `json:"confirm"`
	}
	if !dockerDecode(w, r, &req) {
		return
	}
	device := chi.URLParam(r, "id")
	rt := h.dockerState()
	rt.mu.Lock()
	p, ok := rt.previews[req.PreviewID]
	delete(rt.previews, req.PreviewID)
	rt.mu.Unlock()
	_, actor, _ := dockerActor(r)
	if !ok || p.device != device || p.actor != actor || !p.expires.After(h.clock()) {
		dockerError(w, 409, "preview_expired")
		return
	}
	if len(req.Selected) == 0 || len(req.Selected) > 2000 {
		dockerError(w, 400, "invalid_request")
		return
	}
	items := []map[string]interface{}{}
	seen := map[string]bool{}
	for _, id := range req.Selected {
		item, ok := p.items[id]
		if !ok || seen[id] {
			dockerError(w, 400, "invalid_request")
			return
		}
		seen[id] = true
		items = append(items, item)
	}
	v := h.dockerInventory(r, device)
	if v == nil || h.clock().Sub(v.CollectedAt) > 15*time.Minute {
		dockerError(w, 409, "inventory_stale")
		return
	}
	if p.kind == "volume" {
		d, e := h.devices.GetByID(r.Context(), device)
		if e != nil || d.Hostname != req.Confirm {
			dockerError(w, 400, "confirmation_required")
			return
		}
	} else if len(items) > 20 && req.Confirm != strconv.Itoa(len(items)) {
		dockerError(w, 400, "confirmation_required")
		return
	}
	params := map[string]interface{}{"kind": p.kind, "item_kind": p.kind, "items": items, "preview_id": req.PreviewID}
	res, e := h.cmdSvc().Dispatch(r.Context(), dockerDispatchRequest(r, device, "docker_remove_items", params))
	if e != nil {
		dockerDispatchError(w,e)
		return
	}
	if res.Conflict != nil {
		writeCommandConflict(w, res.Conflict, res.ConflictKey)
		return
	}
	writeJSON(w, 201, res.Command)
}

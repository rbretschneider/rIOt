package handlers

import (
	"context"
	"encoding/json"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/redact"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

func dockerStrings(v interface{}) []string {
	out := []string{}
	switch a := v.(type) {
	case []string:
		return a
	case []interface{}:
		for _, x := range a {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
func dockerPaths(c models.ContainerInfo) (string, []string) {
	wd := c.Labels[models.LabelComposeWorkingDir]
	files := []string{}
	for _, f := range strings.Split(c.Labels["com.docker.compose.project.config_files"], ",") {
		if f = strings.TrimSpace(f); f != "" {
			if !path.IsAbs(f) {
				f = path.Join(wd, f)
			}
			files = append(files, path.Clean(f))
			if len(files) > 10 {
				break
			}
		}
	}
	sort.Strings(files)
	return wd, files
}
func (h *Handlers) dockerStacks(r *http.Request) ([]map[string]interface{}, error) {
	if h.dockerRepo == nil {
		return []map[string]interface{}{}, nil
	}
	known, e := h.dockerRepo.Stacks(r.Context())
	if e != nil {
		return nil, e
	}
	return h.mergeDockerStacks(r.Context(), known)
}

// mergeDockerStacks combines the stored stacks with the stacks observed in the
// latest telemetry of every device, and adds each stack's auto-update state
// and out-of-date member count.
func (h *Handlers) mergeDockerStacks(ctx context.Context, known []map[string]interface{}) ([]map[string]interface{}, error) {
	by := map[string]map[string]interface{}{}
	for _, row := range known {
		key, _ := row["device_id"].(string)
		p, _ := row["project"].(string)
		row["running"] = 0
		row["total"] = 0
		row["state"] = "down"
		row["services"] = []models.ContainerInfo{}
		row["out_of_date"] = 0
		by[key+"/"+p] = row
	}
	ds, e := h.devices.List(ctx)
	if e != nil {
		return nil, e
	}
	projections, e := h.telemetry.GetFleetContainerLeaderboard(ctx)
	if e != nil {
		return nil, e
	}
	// One query for the policies of every host. On failure every row is marked
	// unavailable rather than shown as off.
	var policies autoUpdatePolicyIndex
	policiesUnavailable := false
	if h.autoUpdateRepo != nil {
		all, err := h.autoUpdateRepo.ListAll(ctx)
		if err != nil {
			slog.Warn("docker stacks: auto-update policies unavailable", slog.String("err", err.Error()))
			policiesUnavailable = true
		} else {
			policies = indexAutoUpdatePolicies(all)
		}
	}
	byDevice := map[string][]models.ContainerInfo{}
	for _, projection := range projections {
		for _, c := range projection.Containers {
			byDevice[projection.DeviceID] = append(byDevice[projection.DeviceID], models.ContainerInfo{ID: c.ID, Name: c.Name, ShortID: c.ShortID, Image: c.Image, ImageID: c.ImageID, State: c.State, Status: c.Status, HealthStatus: c.HealthStatus, StartedAt: c.StartedAt, FinishedAt: c.FinishedAt, ExitCode: c.ExitCode, Ports: c.Ports, Labels: c.Labels, CPUPercent: c.CPUPercent, MemUsage: c.MemUsage, MemLimit: c.MemLimit, MemLimitConfigured: c.MemLimitConfigured, RestartCount: c.RestartCount, UpdateAvailable: c.UpdateAvailable, UpdateStatus: c.UpdateStatus, UpdateReason: c.UpdateReason, NetworkMode: c.NetworkMode})
		}
	}
	for _, d := range ds {
		containers := byDevice[d.ID]
		s := &models.TelemetrySnapshot{Timestamp: h.clock(), Data: models.FullTelemetryData{Docker: &models.DockerInfo{Containers: containers}}}
		if d.LastTelemetry != nil {
			s.Timestamp = *d.LastTelemetry
		}
		groups := map[string][]models.ContainerInfo{}
		for _, c := range s.Data.Docker.Containers {
			p := c.Labels[models.LabelComposeProject]
			if dockerName.MatchString(p) {
				groups[p] = append(groups[p], c)
			}
		}
		for project, cs := range groups {
			key := d.ID + "/" + project
			if by[key] != nil && by[key]["forgotten"] == true {
				continue
			}
			by[key] = dockerObservedStack(d, s, project, cs, by[key])
			outOfDate := 0
			for _, c := range cs {
				if containerOutOfDate(c) {
					outOfDate++
				}
			}
			by[key]["out_of_date"] = outOfDate
		}
	}
	for _, row := range by {
		device, _ := row["device_id"].(string)
		project, _ := row["project"].(string)
		row["auto_update"] = policies.stackEnabled(device, project)
		if policiesUnavailable {
			row["auto_update_unavailable"] = true
		} else {
			delete(row, "auto_update_unavailable")
		}
	}
	out := []map[string]interface{}{}
	for _, row := range by {
		if row["forgotten"] != true {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["device_id"].(string)+out[i]["project"].(string) < out[j]["device_id"].(string)+out[j]["project"].(string)
	})
	return out, nil
}
func (h *Handlers) dockerStack(r *http.Request, device, project string) (map[string]interface{}, bool) {
	rows, e := h.dockerStacks(r)
	if e != nil {
		return nil, false
	}
	for _, v := range rows {
		if v["device_id"] == device && v["project"] == project {
			return v, true
		}
	}
	return nil, false
}
func safeStack(row map[string]interface{}, detail bool) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range row {
		if k != "services" && k != "forgotten" && k != "member_names" && k != "members_truncated" {
			out[k] = v
		}
	}
	if detail {
		out["working_dir"] = row["agreed_working_dir"]
		out["config_file_count"] = len(dockerStrings(row["agreed_config_files"]))
		services := []FleetContainerRow{}
		if cs, ok := row["services"].([]models.ContainerInfo); ok {
			for _, c := range cs {
				c = sanitizeDockerContainer(c)
				service := FleetContainerRow{DeviceID: row["device_id"].(string), Hostname: row["hostname"].(string), ContainerID: c.ID, ContainerName: c.Name, ShortID: c.ShortID, Image: c.Image, ImageID: c.ImageID, Stack: c.Labels[models.LabelComposeProject], State: c.State, Status: c.Status, HealthStatus: c.HealthStatus, StartedAt: c.StartedAt, FinishedAt: c.FinishedAt, ExitCode: c.ExitCode, TargetKey: models.TargetKeyContainer(strings.TrimLeft(c.Name, "/")), StackKey: dockerStackKey(c.Labels), ComposeService: c.Labels[models.LabelComposeService], CPUPercent: c.CPUPercent, MemUsage: c.MemUsage, MemLimit: c.MemLimit, MemLimitConfigured: c.MemLimitConfigured, RestartCount: c.RestartCount, Ports: c.Ports, UpdateAvailable: c.UpdateAvailable, UpdateStatus: c.UpdateStatus, UpdateReason: c.UpdateReason, NetworkParent: dockerNetworkParent(c.NetworkMode), SelfRole: dockerSelf(cs)[c.Name]}
				if c.Riot != nil {
					service.RiotName = c.Riot.Name
					service.RiotIcon = c.Riot.Icon
					service.RiotGroup = c.Riot.Group
					service.RiotTags = c.Riot.Tags
				} else {
					service.RiotName = c.Labels["riot.name"]
					service.RiotIcon = c.Labels["riot.icon"]
					service.RiotGroup = c.Labels["riot.group"]
				}
				services = append(services, service)
			}
		}
		out["services"] = services
	}
	return out
}
func (h *Handlers) DockerStacks(w http.ResponseWriter, r *http.Request) {
	rows, e := h.dockerStacks(r)
	if e != nil {
		dockerError(w, 500, "internal")
		return
	}
	out := []map[string]interface{}{}
	for _, row := range rows {
		out = append(out, safeStack(row, false))
	}
	writeJSON(w, 200, out)
}
func (h *Handlers) DockerStackDetail(w http.ResponseWriter, r *http.Request) {
	row, ok := h.dockerStack(r, chi.URLParam(r, "deviceId"), chi.URLParam(r, "project"))
	if !ok {
		dockerError(w, 404, "stack_not_found")
		return
	}
	writeJSON(w, 200, safeStack(row, true))
}
func (h *Handlers) DockerStackAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action      string `json:"action"`
		Confirm     string `json:"confirm"`
		ConfirmHost string `json:"confirm_host"`
	}
	if !dockerDecode(w, r, &req) {
		return
	}
	device, project := chi.URLParam(r, "id"), chi.URLParam(r, "project")
	row, ok := h.dockerStack(r, device, project)
	if !ok {
		dockerError(w, 404, "stack_not_found")
		return
	}
	actions := map[string]string{"pull": "docker_stack_pull", "update": "docker_update", "redeploy": "docker_stack_redeploy", "restart": "docker_stack_restart", "down": "docker_stack_down", "up": "docker_stack_up"}
	action := actions[req.Action]
	if action == "" {
		dockerError(w, 400, "invalid_request")
		return
	}
	if row["path_state"] != "agreed" {
		dockerError(w, 409, "stack_paths_unagreed")
		return
	}
	if req.Action == "down" && req.Confirm != project {
		dockerError(w, 400, "confirmation_required")
		return
	}
	if (req.Action == "down" || req.Action == "redeploy" || req.Action == "restart") && row["self_role"] != nil && req.ConfirmHost != row["hostname"] {
		dockerError(w, 400, "confirmation_required")
		return
	}
	if !h.dockerCap(w, r, device, models.DockerCapStackActions) {
		return
	}
	params := map[string]interface{}{"compose_project": project, "compose_work_dir": row["agreed_working_dir"], "compose_config_files": row["agreed_config_files"]}
	res, e := h.cmdSvc().Dispatch(r.Context(), dockerDispatchRequest(r, device, action, params))
	if e != nil {
		dockerDispatchError(w, e)
		return
	}
	if res.Conflict != nil {
		writeCommandConflict(w, res.Conflict, res.ConflictKey)
		return
	}
	writeJSON(w, 201, res.Command)
}
func (h *Handlers) DockerForgetStack(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !dockerDecode(w, r, &req) {
		return
	}
	device, project := chi.URLParam(r, "id"), chi.URLParam(r, "project")
	row, ok := h.dockerStack(r, device, project)
	if !ok {
		dockerError(w, 404, "stack_not_found")
		return
	}
	if row["state"] != "down" {
		dockerError(w, 409, "not_down")
		return
	}
	row["forgotten"] = true
	delete(row, "services")
	if h.dockerRepo.SaveStack(r.Context(), device, project, row) != nil {
		dockerError(w, 500, "internal")
		return
	}
	w.WriteHeader(204)
}
func (h *Handlers) DockerAcknowledgePaths(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkingDir  string   `json:"working_dir"`
		ConfigFiles []string `json:"config_files"`
	}
	if !dockerDecode(w, r, &req) {
		return
	}
	device, project := chi.URLParam(r, "id"), chi.URLParam(r, "project")
	row, ok := h.dockerStack(r, device, project)
	if !ok {
		dockerError(w, 404, "stack_not_found")
		return
	}
	if row["path_state"] != "pending_ack" {
		dockerError(w, 409, "not_pending")
		return
	}
	if req.WorkingDir != row["observed_working_dir"] || !reflect.DeepEqual(req.ConfigFiles, dockerStrings(row["observed_config_files"])) {
		dockerError(w, 409, "paths_mismatch")
		return
	}
	raw, status := h.dockerRPC(r, device, map[string]interface{}{"kind": "acknowledge_paths", "project": project, "working_dir": req.WorkingDir, "config_files": req.ConfigFiles})
	if status != 200 {
		dockerError(w, status, map[int]string{502: "agent_offline", 504: "agent_timeout", 429: "rate_limited"}[status])
		return
	}
	var ack map[string]interface{}
	if json.Unmarshal(raw, &ack) != nil || ack["error"] != nil {
		dockerError(w, 409, "paths_mismatch")
		return
	}
	row["agreed_working_dir"] = req.WorkingDir
	row["agreed_config_files"] = req.ConfigFiles
	row["path_state"] = "agreed"
	delete(row, "observed_working_dir")
	delete(row, "observed_config_files")
	delete(row, "services")
	if h.dockerRepo.SaveStack(r.Context(), device, project, row) != nil {
		dockerError(w, 500, "internal")
		return
	}
	_, sub, _ := dockerActor(r)
	h.dockerRepo.Audit(r.Context(), device, "stack_paths", "acknowledge", sub, map[string]interface{}{"project": project})
	writeJSON(w, 200, map[string]string{"path_state": "agreed"})
}
func (h *Handlers) dockerCap(w http.ResponseWriter, r *http.Request, device, cap string) bool {
	s, ok := h.dockerSnapshot(r, device)
	if ok {
		for _, v := range s.Data.Docker.Capabilities {
			if v == cap {
				return true
			}
		}
	}
	writeJSON(w, 409, map[string]string{"error": "agent unsupported", "code": "agent_unsupported", "capability": cap})
	return false
}
func (h *Handlers) dockerReadRequest(w http.ResponseWriter, r *http.Request) {
	device := chi.URLParam(r, "id")
	name, project := chi.URLParam(r, "name"), chi.URLParam(r, "project")
	data := map[string]interface{}{}
	kind := "inspect"
	cap := models.DockerCapInspect
	if name != "" {
		if !dockerName.MatchString(name) {
			dockerError(w, 400, "invalid_identifier")
			return
		}
		s, ok := h.dockerSnapshot(r, device)
		found := false
		if ok {
			for _, c := range s.Data.Docker.Containers {
				if strings.TrimLeft(c.Name, "/") == name {
					found = true
				}
			}
		}
		if !found {
			dockerError(w, 404, "container_not_found")
			return
		}
		data["container"] = name
	} else {
		cap = models.DockerCapComposeRead
		row, ok := h.dockerStack(r, device, project)
		if !ok {
			dockerError(w, 404, "stack_not_found")
			return
		}
		if row["path_state"] != "agreed" {
			dockerError(w, 409, "stack_paths_unagreed")
			return
		}
		kind = "compose"
		if strings.HasSuffix(r.URL.Path, "/env") {
			kind = "env"
		}
		if i := chi.URLParam(r, "index"); i != "" {
			n, e := strconv.Atoi(i)
			if e != nil || n < 0 || n >= len(dockerStrings(row["agreed_config_files"])) {
				dockerError(w, 400, "invalid_request")
				return
			}
			kind = "compose_file"
			data["index"] = n
		}
		data["project"] = project
		data["working_dir"] = row["agreed_working_dir"]
		data["config_files"] = row["agreed_config_files"]
	}
	if !h.dockerCap(w, r, device, cap) {
		return
	}
	data["kind"] = kind
	raw, status := h.dockerRPC(r, device, data)
	if status != 200 {
		code := map[int]string{502: "agent_offline", 504: "agent_timeout", 429: "rate_limited"}[status]
		dockerError(w, status, code)
		return
	}
	var out map[string]interface{}
	if json.Unmarshal(raw, &out) != nil {
		dockerError(w, 422, "read_refused")
		return
	}
	if out["error"] != nil {
		writeJSON(w, 422, map[string]interface{}{"error": "read refused", "code": "read_refused", "reason": dockerReadReason(out["reason"])})
		return
	}
	if kind == "inspect" {
		var text string
		if v, ok := out["json"].(string); ok {
			text = v
		} else {
			b, _ := json.MarshalIndent(out["inspect"], "", "  ")
			text = string(b)
		}
		masked, n, e := redact.InspectJSON([]byte(text))
		if e != nil {
			dockerError(w, 422, "read_refused")
			return
		}
		out = map[string]interface{}{"json": string(masked), "redactions": n, "fetched_at": h.clock()}
	} else if kind == "compose_file" {
		content, ok := out["content"].(string)
		if !ok {
			dockerError(w, 422, "read_refused")
			return
		}
		safe, n, e := redact.ComposeFile([]byte(content))
		if e != nil {
			dockerError(w, 422, "read_refused")
			return
		}
		out["content"] = string(safe)
		out["redactions"] = n
		out["line_count"] = strings.Count(string(safe), "\n") + 1
	} else if kind == "env" {
		rawKeys := out["keys"]
		if rawKeys == nil {
			rawKeys = out["entries"]
		}
		raw, _ := json.Marshal(rawKeys)
		var keys []redact.EnvKey
		if json.Unmarshal(raw, &keys) != nil || len(keys) > 4096 {
			dockerError(w, 422, "read_refused")
			return
		}
		count := 0
		for i := range keys {
			if len(keys[i].Key) > 512 || len(keys[i].Value) > 16384 {
				dockerError(w, 422, "read_refused")
				return
			}
			safe, masked := redact.EnvPair(keys[i].Key, keys[i].Value)
			keys[i].Value = safe
			if masked {
				count++
			}
		}
		out = map[string]interface{}{"keys": keys, "redactions": count}
	} else {
		out["read_only"] = true
		out["path_state"] = "agreed"
	}
	if h.dockerRepo != nil {
		_, sub, _ := dockerActor(r)
		auditKind := "compose"
		if kind == "inspect" {
			auditKind = "inspect"
		}
		h.dockerRepo.Audit(r.Context(), device, auditKind, "read", sub, map[string]interface{}{"container": name, "stack": project, "ended_at": h.clock()})
	}

	writeJSON(w, 200, out)
}

func dockerReadReason(v interface{}) string {
	s, _ := v.(string)
	switch s {
	case "not_found", "outside_stack_directory", "denied_path", "bad_extension", "too_large", "not_readable", "missing", "redaction_failed", "unsupported", "too_many_files", "conflicted", "paths_changed", "container_writable", "not_regular":
		return s
	case "path_mismatch":
		return "paths_changed"
	case "parse_failed":
		return "redaction_failed"
	default:
		return "not_readable"
	}
}

func validStackPath(v string) bool {
	if !path.IsAbs(v) || len(v) > 1024 {
		return false
	}
	for _, r := range v {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func dockerObservedStack(d models.Device, s *models.TelemetrySnapshot, project string, cs []models.ContainerInfo, row map[string]interface{}) map[string]interface{} {
	if row == nil {
		row = map[string]interface{}{"device_id": d.ID, "hostname": d.Hostname, "project": project, "auto_update": false}
	}
	row["forgotten"] = false
	row["total"] = len(cs)
	names := []string{}
	for i, c := range cs {
		if i >= 25 {
			break
		}
		names = append(names, strings.TrimLeft(c.Name, "/"))
	}
	row["member_names"] = names
	row["members_truncated"] = len(cs) > 25
	row["services"] = cs
	row["last_seen_at"] = s.Timestamp
	running, updates := 0, 0
	for _, c := range cs {
		if c.State == "running" {
			running++
		}
		if c.UpdateAvailable != nil && *c.UpdateAvailable {
			updates++
		}
		if role := dockerSelf(s.Data.Docker.Containers)[c.Name]; role != "" {
			row["self_role"] = role
		}
	}
	row["running"] = running
	row["updates"] = updates
	state := "partial"
	if running == len(cs) {
		state = "running"
	} else if running == 0 {
		state = "down"
	}
	row["state"] = state
	wd, files := dockerPaths(cs[0])
	conflicts := []string{}
	for _, c := range cs {
		cw, cf := dockerPaths(c)
		if cw != wd || !reflect.DeepEqual(cf, files) {
			if len(conflicts) < 20 {
				conflicts = append(conflicts, c.Name)
			}
		}
	}
	for _, file := range files {
		if !validStackPath(file) {
			conflicts = append(conflicts, "invalid_path")
			break
		}
	}
	row["conflict_members"] = conflicts
	if len(conflicts) > 0 || !validStackPath(wd) || len(files) == 0 || len(files) > 10 || len(wd) > 1024 {
		row["path_state"] = "conflicted"
	} else {
		old, _ := row["agreed_working_dir"].(string)
		if old == "" {
			row["agreed_working_dir"] = wd
			row["agreed_config_files"] = files
			row["path_state"] = "agreed"
		} else if old != wd || !reflect.DeepEqual(dockerStrings(row["agreed_config_files"]), files) {
			row["path_state"] = "pending_ack"
			row["observed_working_dir"] = wd
			row["observed_config_files"] = files
		} else {
			row["path_state"] = "agreed"
			delete(row, "observed_working_dir")
			delete(row, "observed_config_files")
		}
	}
	return row
}

// ObserveDockerSnapshot persists agreed stack identity before services disappear.
func (h *Handlers) ObserveDockerSnapshot(ctx context.Context, deviceID string, s *models.TelemetrySnapshot) {
	if h.dockerRepo == nil || s == nil || s.Data.Docker == nil {
		return
	}
	d, err := h.devices.GetByID(ctx, deviceID)
	if err != nil || d == nil {
		return
	}
	groups := map[string][]models.ContainerInfo{}
	for _, c := range s.Data.Docker.Containers {
		p := c.Labels[models.LabelComposeProject]
		if dockerName.MatchString(p) {
			groups[p] = append(groups[p], c)
		}
	}
	known := map[string]map[string]interface{}{}
	if rows, err := h.dockerRepo.StacksForDevice(ctx, deviceID); err == nil {
		for _, row := range rows {
			project, _ := row["project"].(string)
			known[project] = row
		}
	}
	for project, cs := range groups {
		row := known[project]
		delete(known, project)
		row = dockerObservedStack(*d, s, project, cs, row)
		delete(row, "services")
		if err := h.dockerRepo.ObserveStack(ctx, deviceID, project, row); err != nil {
			return
		}
	}
	for project, row := range known {
		row["state"] = "down"
		row["running"] = 0
		row["total"] = 0
		delete(row, "services")
		if h.dockerRepo.ObserveStack(ctx, deviceID, project, row) != nil {
			return
		}
	}

}

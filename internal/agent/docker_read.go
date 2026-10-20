package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/redact"
	"github.com/docker/docker/api/types/container"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type dockerReadRequest struct {
	Kind        string   `json:"kind"`
	Container   string   `json:"container"`
	Project     string   `json:"project"`
	Index       int      `json:"index"`
	WorkingDir  string   `json:"working_dir"`
	ConfigFiles []string `json:"config_files"`
}

func (a *Agent) handleDockerRead(ctx context.Context, msg AgentWSMessage) {
	var req dockerReadRequest
	response := map[string]interface{}{}
	defer func() {
		b, _ := json.Marshal(response)
		limit := 1 << 20
		if req.Kind == "env" {
			limit = 256 << 10
		}
		if len(b) > limit {
			b = []byte(`{"error":"too_large","reason":"too_large"}`)
		}
		a.sendWS(AgentWSMessage{Type: "docker_read_result", SessionID: msg.SessionID, Data: b})
	}()
	fail := func(reason string) { response = map[string]interface{}{"ok": false, "error": reason, "reason": reason} }
	if len(msg.Data) > 4096 || json.Unmarshal(msg.Data, &req) != nil {
		fail("invalid_request")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	api, err := a.newDocker()
	if err != nil {
		fail("docker_unavailable")
		return
	}
	defer api.Close()
	if req.Kind == "inspect" {
		if !dockerNamePattern.MatchString(req.Container) {
			fail("invalid_identifier")
			return
		}
		insp, err := api.ContainerInspect(ctx, req.Container)
		if err != nil {
			fail("not_found")
			return
		}
		raw, _ := json.Marshal(insp)
		if len(raw) > 1<<20 {
			fail("too_large")
			return
		}
		safe, n, err := redact.InspectJSON(raw)
		if err != nil {
			fail("redaction_failed")
			return
		}
		response = map[string]interface{}{"ok": true, "json": string(safe), "redactions": n, "fetched_at": time.Now().UTC()}
		return
	}
	if runtime.GOOS != "linux" {
		fail("unsupported")
		return
	}
	if !composeProjectRe.MatchString(req.Project) {
		fail("invalid_identifier")
		return
	}
	snap, err := loadDaemonSnapshot(ctx, api)
	if err != nil {
		fail("docker_unavailable")
		return
	}
	snap = a.composeTrust().bind(snap)
	snap, td, err := a.resolveStackPaths(snap, req.Project, req.WorkingDir, req.ConfigFiles, req.Kind == "acknowledge_paths")
	if err != nil {
		reason := "paths_changed"
		var trustErr *composeTrustError
		if errors.As(err, &trustErr) && trustErr.Rule == ruleContainerWritable {
			reason = "container_writable"
		}
		if strings.Contains(err.Error(), "conflict") {
			reason = "conflicted"
		}
		fail(reason)
		return
	}
	if req.Kind == "acknowledge_paths" {
		response = map[string]interface{}{"acknowledged": true}
		return
	}
	files := td.Files()
	if len(req.ConfigFiles) > 0 {
		if strings.Join(req.ConfigFiles, "\x00") != strings.Join(files, "\x00") {
			fail("paths_changed")
			return
		}
	}
	switch req.Kind {
	case "compose", "compose_list":
		trust := a.composeTrust()
		trust.trustedRoots = nil
		writable := trust.strictWritable(snap, td.Path()) != nil
		meta := make([]map[string]interface{}, 0, len(files))
		for i, file := range files {
			raw, err := secureDockerRead(file)
			status := "ok"
			if err != nil {
				status = dockerReadErrorReason(err)
			}
			meta = append(meta, map[string]interface{}{"index": i, "name": filepath.Base(file), "size": len(raw), "status": status, "container_writable": writable})
		}
		envRaw, envErr := secureDockerRead(filepath.Join(td.Path(), ".env"))
		envStatus := "ok"
		if envErr != nil {
			envStatus = dockerReadErrorReason(envErr)
		} else if len(envRaw) > 256<<10 {
			envStatus = "too_large"
		}
		response = map[string]interface{}{"ok": true, "working_dir": td.Path(), "read_only": true, "path_state": "agreed", "container_writable": writable, "files": meta, "env": map[string]interface{}{"present": !errors.Is(envErr, os.ErrNotExist), "status": envStatus}}
	case "compose_file":
		if req.Index < 0 || req.Index >= len(files) {
			fail("invalid_index")
			return
		}
		raw, err := secureDockerRead(files[req.Index])
		if err != nil {
			fail(dockerReadErrorReason(err))
			return
		}
		safe, n, err := redact.ComposeFile(raw)
		if err != nil {
			fail("redaction_failed")
			return
		}
		response = map[string]interface{}{"ok": true, "name": filepath.Base(files[req.Index]), "content": string(safe), "redactions": n, "line_count": strings.Count(string(safe), "\n") + 1}
	case "env":
		raw, err := secureDockerRead(filepath.Join(td.Path(), ".env"))
		if err != nil {
			fail(dockerReadErrorReason(err))
			return
		}
		if len(raw) > 256<<10 {
			fail("too_large")
			return
		}
		keys, n, err := redact.DotEnv(raw)
		if err != nil {
			fail("redaction_failed")
			return
		}
		response = map[string]interface{}{"ok": true, "keys": keys, "redactions": n}
	default:
		fail("unsupported")
	}
}

func dockerReadErrorReason(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "not_found"
	case errors.Is(err, os.ErrPermission):
		return "not_readable"
	case errors.Is(err, errFileTooLarge):
		return "too_large"
	case strings.Contains(err.Error(), "regular file"):
		return "not_regular"
	default:
		return "outside_stack_directory"
	}
}
func (a *Agent) dockerStackAction(cr *cmdRun) (result commandExecResult) {
	cr.Step(models.StepPrecheck)
	project, _ := cr.payload.Params["compose_project"].(string)
	if project == "" {
		project, _ = cr.payload.Params["project"].(string)
	}
	dir, _ := cr.payload.Params["compose_work_dir"].(string)
	if dir == "" {
		dir, _ = cr.payload.Params["working_dir"].(string)
	}
	if !composeProjectRe.MatchString(project) {
		return precheckFailure("invalid compose project")
	}
	api, err := a.newDocker()
	if err != nil {
		return precheckFailure("docker unavailable")
	}
	defer api.Close()
	snap, err := loadDaemonSnapshot(cr.ctx, api)
	if err != nil {
		return precheckFailure("cannot verify Docker state")
	}
	snap = a.composeTrust().bind(snap)
	var files []string
	if raw, e := json.Marshal(cr.payload.Params["compose_config_files"]); e == nil {
		json.Unmarshal(raw, &files)
	}
	snap, td, err := a.resolveStackPaths(snap, project, dir, files, false)
	if err != nil {
		return precheckFailure(err.Error())
	}
	dir = td.Path()
	keys := []string{models.TargetKeyCompose(dir)}
	for _, ctr := range snap.Containers {
		if ctr.Labels[models.LabelComposeProject] == project {
			keys = append(keys, models.TargetKeyContainer(summaryName(ctr)))
		}
	}
	if key, holder, ok := cr.lock(keys...); !ok {
		return conflictResult(key, holder)
	}
	// Resolve again after acquiring the member locks, before touching Docker.
	fresh, err := loadDaemonSnapshot(cr.ctx, api)
	if err != nil {
		return precheckFailure("cannot recheck Docker state")
	}
	fresh, checked, err := a.resolveStackPaths(fresh, project, dir, td.Files(), false)
	if err != nil || !checked.same(td) {
		return precheckFailure("stack paths changed during precheck")
	}
	snap = fresh
	scope := scopeOf(snap.Containers, snap.excluded, snap.resolve, project, []string{dir, td.Path()}, "")
	wasRunning := map[string]bool{}
	for _, ctr := range scope {
		if ctr.State == "running" {
			wasRunning[ctr.Labels[models.LabelComposeService]] = true
		}
	}
	deps := &depState{}
	defer func() { result = cr.finishDependents(deps, result) }()
	if cr.payload.Action == "docker_stack_restart" || cr.payload.Action == "docker_stack_redeploy" {
		var running []depRecord
		for _, dep := range findDependents(snap.Containers, scope) {
			if dep.Running {
				running = append(running, dep)
			}
		}
		var depKeys []string
		for _, dep := range running {
			depKeys = append(depKeys, models.TargetKeyContainer(dep.Name))
		}
		if key, h, ok := cr.lock(depKeys...); !ok {
			return conflictResult(key, h)
		}
		if failure := cr.stopDependents(api, deps, "compose:"+td.Path(), running); failure != nil {
			return *failure
		}
	}
	var sub []string
	step := "up"
	switch cr.payload.Action {
	case "docker_stack_pull":
		sub = []string{"pull"}
		step = "pull"
	case "docker_stack_restart":
		sub = []string{"restart"}
		step = "restart"
	case "docker_stack_down":
		sub = []string{"down"}
		step = "down"
	case "docker_stack_up":
		sub = []string{"up", "-d", "--no-build", "--pull=missing"}
	case "docker_stack_redeploy":
		sub = []string{"up", "-d", "--no-build", "--force-recreate", "--pull=never"}
		step = models.StepRecreate
	default:
		return precheckFailure("unsupported stack action")
	}
	args, err := composeStackArgs(td, project, sub)
	if err != nil {
		return precheckFailure(err.Error())
	}
	cr.Step(step)
	out, timedOut, err := cr.runCompose(args, 10*time.Minute)
	masked, _ := redact.MaskText(string(out), "<redacted>")
	if err != nil || timedOut {
		return commandExecResult{Status: "error", Step: step, Message: fmt.Sprintf("stack %s failed", step), Output: masked}
	}
	var targets []models.DockerTargetResult
	if cr.payload.Action == "docker_stack_up" || cr.payload.Action == "docker_stack_restart" || cr.payload.Action == "docker_stack_redeploy" {
		cr.Verifying(models.StepVerify)
		verifyCtx, cancel := context.WithTimeout(cr.ctx, models.VerifyBudget)
		defer cancel()
		if !a.pauseFor(verifyCtx, models.VerifyWindow) {
			return commandExecResult{Status: "error", Step: models.StepVerify, Message: "stack verification interrupted", Output: masked}
		}
		after, err := api.ContainerList(verifyCtx, container.ListOptions{All: true})
		if err != nil {
			return commandExecResult{Status: "error", Step: models.StepVerify, Message: "cannot verify stack state", Output: masked}
		}
		post := scopeOf(after, nil, snap.resolve, project, []string{dir, td.Path()}, "")
		if len(post) == 0 {
			return commandExecResult{Status: "error", Step: models.StepVerify, Message: "stack has no services after action", Output: masked}
		}
		for service := range wasRunning {
			running := false
			for _, ctr := range post {
				if ctr.Labels[models.LabelComposeService] == service && ctr.State == "running" {
					running = true
					break
				}
			}
			outcome := "succeeded"
			if !running {
				outcome = "failed"
			}
			targets = append(targets, models.DockerTargetResult{Container: service, Outcome: outcome, Step: models.StepVerify, Message: "service " + outcome})
			if !running {
				return commandExecResult{Status: "error", Step: models.StepVerify, Message: "a previously running service did not recover", Output: masked, TargetResults: targets}
			}
		}
		if len(wasRunning) == 0 {
			running := false
			for _, ctr := range post {
				if ctr.State == "running" {
					running = true
					targets = append(targets, models.DockerTargetResult{Container: ctr.Labels[models.LabelComposeService], Outcome: "succeeded", Step: models.StepVerify})
				}
			}
			if !running {
				return commandExecResult{Status: "error", Step: models.StepVerify, Message: "no stack service is running", Output: masked}
			}
		}
	}
	if cr.payload.Action == "docker_stack_redeploy" {
		var previous []string
		for _, ctr := range snap.Containers {
			if ctr.Labels[models.LabelComposeProject] == project {
				previous = append(previous, ctr.ImageID)
			}
		}
		cr.cleanupSupersededImages(previous)
	}
	a.triggerTelemetry()
	return commandExecResult{Status: "success", Step: step, Message: "stack action completed", Output: masked, TargetResults: targets}
}
func composeStackArgs(td trustedDir, project string, sub []string) ([]string, error) {
	if !td.valid() || !composeProjectRe.MatchString(project) {
		return nil, fmt.Errorf("untrusted compose target")
	}
	args := []string{"compose", "--project-directory=" + td.Path(), "--project-name=" + project}
	for _, f := range td.Files() {
		args = append(args, "-f", f)
	}
	return append(args, sub...), nil
}

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// updateTarget is what a docker_update names: a container by name (preferred,
// BR-001), by ID (legacy servers), or a whole compose stack by directory.
type updateTarget struct {
	Name    string
	ID      string
	WorkDir string
}

func targetFromParams(params map[string]interface{}) updateTarget {
	name, _ := params["container"].(string)
	id, _ := params["container_id"].(string)
	wd, _ := params["compose_work_dir"].(string)
	return updateTarget{Name: strings.TrimPrefix(name, "/"), ID: id, WorkDir: wd}
}

// updateOpts tunes one inner update. A bulk update pre-locks the union of its
// targets and runs each inner update with lockHeld.
type updateOpts struct {
	lockHeld bool
}

// dockerUpdate pulls a newer image and recreates the container, or updates a
// compose stack. Compose containers delegate to `docker compose`; standalone
// containers use the Docker SDK with rename-aside rollback (AD-014).
func (a *Agent) dockerUpdate(cr *cmdRun) commandExecResult {
	t := targetFromParams(cr.payload.Params)
	if t.Name == "" && t.ID == "" && t.WorkDir == "" {
		return precheckFailure("container, container_id or compose_work_dir is required")
	}
	api, err := a.newDocker()
	if err != nil {
		return precheckFailure(fmt.Sprintf("docker client: %s", err))
	}
	defer api.Close()

	res, refs := cr.dockerUpdateOne(api, t, updateOpts{})
	a.invalidateFreshness(refs)
	a.triggerTelemetry()
	return res
}

func precheckFailure(msg string) commandExecResult {
	return commandExecResult{Status: models.ResultStatusError, Step: models.StepPrecheck, Message: msg}
}

func persistenceFailure(step, what string, err error) commandExecResult {
	return commandExecResult{
		Status:  models.ResultStatusError,
		Step:    step,
		Reason:  models.ReasonPersistence,
		Message: fmt.Sprintf("%s: in-flight marker could not be written, aborting before the next Docker change", what),
		Stderr:  err.Error(),
	}
}

// restartInProgress refuses a Docker update while agent_update or
// agent_uninstall holds the host lock: the restart would cut the recreate in
// half (SEC C-4c, symmetric check).
func (cr *cmdRun) restartInProgress() (commandExecResult, bool) {
	h, ok := cr.a.locks.Holder(models.TargetKeyHost)
	if !ok || (h.Action != models.ActionAgentUpdate && h.Action != models.ActionAgentUninstall) {
		return commandExecResult{}, false
	}
	return commandExecResult{
		Status:  models.ResultStatusError,
		Step:    models.StepPrecheck,
		Reason:  models.ReasonConflict,
		Message: fmt.Sprintf("agent restart in progress (command %s); refusing docker update", h.CmdID),
	}, true
}

func lifecycleAction(res commandExecResult) string {
	if res.Status == models.ResultStatusError {
		return "update_failed"
	}
	return "update_completed"
}

// dockerUpdateOne updates one target and returns the image references involved,
// before and after, for cache invalidation (FR-052).
func (cr *cmdRun) dockerUpdateOne(api dockerAPI, t updateTarget, o updateOpts) (commandExecResult, []string) {
	cr.Step(models.StepPrecheck)
	if r, bad := cr.restartInProgress(); bad {
		return r, nil
	}

	// Full-stack update: no container to resolve.
	if t.Name == "" && t.ID == "" {
		return cr.updateStack(api, t.WorkDir, o)
	}

	// The container lock is taken before any Docker call (AD-012). Without a
	// name yet, the ID stands in until the container is resolved.
	if !o.lockHeld {
		pre := models.TargetKeyContainerID(t.ID)
		if t.Name != "" {
			pre = models.TargetKeyContainer(t.Name)
		}
		if key, holder, ok := cr.lock(pre); !ok {
			return conflictResult(key, holder), nil
		}
	}

	cr.Step(models.StepResolve)
	insp, err := resolveContainer(cr.ctx, api, t)
	if err != nil {
		return commandExecResult{
			Status:  models.ResultStatusError,
			Step:    models.StepResolve,
			Message: err.Error(),
		}, nil
	}
	name := strings.TrimPrefix(insp.Name, "/")
	labels := map[string]string{}
	if insp.Config != nil && insp.Config.Labels != nil {
		labels = insp.Config.Labels
	}
	project := labels[models.LabelComposeProject]
	service := labels[models.LabelComposeService]
	workDir := labels[models.LabelComposeWorkingDir]
	isCompose := project != "" && service != "" && workDir != ""

	if !o.lockHeld {
		keys := []string{models.TargetKeyContainer(name)}
		if isCompose {
			keys = append(keys, models.TargetKeyCompose(workDir))
		}
		if key, holder, ok := cr.lock(keys...); !ok {
			return conflictResult(key, holder), nil
		}
	}

	if isCompose {
		return cr.composeUpdate(api, composeSpec{WorkDir: workDir, Service: service})
	}
	return cr.standaloneUpdate(api, insp)
}

// resolveContainer finds the container to update. A name wins over an ID, and a
// leading "/" never matters (FR-002).
func resolveContainer(ctx context.Context, api dockerAPI, t updateTarget) (container.InspectResponse, error) {
	pctx, cancel := context.WithTimeout(ctx, models.DockerPrecheckBudget)
	defer cancel()

	target := t.ID
	if t.Name != "" {
		target = t.Name
		all, err := api.ContainerList(pctx, container.ListOptions{All: true})
		if err != nil {
			return container.InspectResponse{}, fmt.Errorf("list containers: %w", err)
		}
		c, ok := findByName(all, t.Name)
		if !ok {
			return container.InspectResponse{}, fmt.Errorf("container %q not found", target)
		}
		insp, err := api.ContainerInspect(pctx, c.ID)
		if err != nil {
			return container.InspectResponse{}, fmt.Errorf("container %q not found: %w", target, err)
		}
		return insp, nil
	}
	insp, err := api.ContainerInspect(pctx, t.ID)
	if err != nil {
		return container.InspectResponse{}, fmt.Errorf("container %q not found: %w", target, err)
	}
	return insp, nil
}

// ---------------------------------------------------------------------------
// Dependents
// ---------------------------------------------------------------------------

// depState is the set of dependents an update has stopped (or is about to), so
// they can be restored whatever the outcome (FR-030).
type depState struct {
	api  dockerAPI
	deps []depRecord
}

// stopDependents records the dependents durably, then stops them within
// StopDependentsBudget. The state is registered before the first stop, so the
// deferred restore covers a partial stop. A marker failure aborts before any
// container is touched (SEC C-4a).
func (cr *cmdRun) stopDependents(api dockerAPI, st *depState, markerKey string, deps []depRecord) *commandExecResult {
	if len(deps) == 0 {
		return nil
	}
	cr.Step(models.StepStopDependents)
	ids := make([]string, len(deps))
	for i, d := range deps {
		ids[i] = d.ID
	}
	if err := cr.markDocker(markerKey, func(r *dockerRecord) { r.StoppedDependentIDs = ids }); err != nil {
		res := persistenceFailure(models.StepStopDependents, "stop network dependents", err)
		return &res
	}
	st.api, st.deps = api, deps

	ctx, cancel := context.WithTimeout(cr.ctx, models.StopDependentsBudget)
	defer cancel()
	timeout := 10
	for _, d := range deps {
		if err := api.ContainerStop(ctx, d.ID, container.StopOptions{Timeout: &timeout}); err != nil {
			slog.Warn("docker_update: failed to stop network dependent", "container", d.Name, "error", err)
		}
	}
	return nil
}

// finishDependents restores every stopped dependent on its own 2-minute budget
// and turns a failed restore into a failed command (FR-032, FR-033). It runs
// deferred, so it covers failures, timeouts and recovered panics.
func (cr *cmdRun) finishDependents(st *depState, res commandExecResult) commandExecResult {
	if st == nil || len(st.deps) == 0 {
		return res
	}
	cr.Step(models.StepRestartDependents)
	ctx, cancel := context.WithTimeout(context.Background(), models.DependentRestoreBudget)
	defer cancel()

	failed := cr.a.restoreDependents(ctx, st.api, st.deps)
	if len(failed) == 0 {
		return res
	}
	outcome := "succeeded"
	if res.Status == models.ResultStatusError {
		outcome = "failed"
		if res.Step != "" {
			outcome += " at " + res.Step
		}
	}
	slog.Error("docker_update: dependents not restored", "command_id", cr.id, "dependents", failureNames(failed), "main_update", outcome)
	stderr := failureReasons(failed)
	if res.Status == models.ResultStatusError && res.Message != "" {
		stderr += "\nmain update: " + res.Message
	}
	return commandExecResult{
		Status:  models.ResultStatusError,
		Step:    models.StepRestartDependents,
		Message: fmt.Sprintf("dependents not restored: %s; main update %s", failureNames(failed), outcome),
		Stderr:  stderr,
		Output:  res.Output,
	}
}

// ---------------------------------------------------------------------------
// Compose
// ---------------------------------------------------------------------------

type composeSpec struct {
	WorkDir string
	Service string // empty = the whole stack
}

func (s composeSpec) label() string {
	if s.Service != "" {
		return s.Service
	}
	return filepath.Base(s.WorkDir) + " stack"
}

func (s composeSpec) markerKey() string { return "compose:" + s.WorkDir }

// composeArgs builds the argv of one `docker compose` invocation (SEC C-9): the
// project directory and the optional sanitized file as single "=" tokens, the
// sub-command and its flags, then "--" and the service names. Service names are
// validated against the compose charset, and the working directory must be
// absolute, so no label value can be read as a flag.
func composeArgs(workDir, sanitizedFile string, sub []string, services ...string) ([]string, error) {
	if !filepath.IsAbs(workDir) {
		return nil, fmt.Errorf("compose working directory %q is not an absolute path", workDir)
	}
	for _, s := range services {
		if !composeServiceRe.MatchString(s) {
			return nil, fmt.Errorf("invalid compose service name %q", s)
		}
	}
	args := []string{"compose", "--project-directory=" + workDir}
	if sanitizedFile != "" {
		args = append(args, "-f="+sanitizedFile)
	}
	args = append(args, sub...)
	if len(services) > 0 {
		args = append(args, "--")
		args = append(args, services...)
	}
	return args, nil
}

// isComposeValidationError checks if compose output indicates a file validation
// error (e.g. "services.deploy additional properties 'resources' not allowed").
func isComposeValidationError(output string) bool {
	return strings.Contains(output, "validating") && strings.Contains(output, "not allowed")
}

// updateStack validates a full-stack target and updates it.
func (cr *cmdRun) updateStack(api dockerAPI, workDir string, o updateOpts) (commandExecResult, []string) {
	if !filepath.IsAbs(workDir) {
		return precheckFailure(fmt.Sprintf("compose_work_dir %q is not an absolute path", workDir)), nil
	}
	if !o.lockHeld {
		if key, holder, ok := cr.lock(models.TargetKeyCompose(workDir)); !ok {
			return conflictResult(key, holder), nil
		}
	}
	// The directory must belong to a real compose project: a forged command
	// cannot point compose at an arbitrary directory (SEC C-9).
	pctx, cancel := context.WithTimeout(cr.ctx, models.DockerPrecheckBudget)
	defer cancel()
	all, err := api.ContainerList(pctx, container.ListOptions{All: true})
	if err != nil {
		return precheckFailure(fmt.Sprintf("list containers: %s", err)), nil
	}
	if len(projectContainers(all, workDir, "")) == 0 {
		return precheckFailure(fmt.Sprintf("no container has compose working directory %q", workDir)), nil
	}
	return cr.composeUpdate(api, composeSpec{WorkDir: workDir})
}

// projectContainers returns the containers of a compose project, optionally
// narrowed to one service.
func projectContainers(all []container.Summary, workDir, service string) []container.Summary {
	var out []container.Summary
	for _, c := range all {
		if models.TargetKeyCompose(c.Labels[models.LabelComposeWorkingDir]) != models.TargetKeyCompose(workDir) {
			continue
		}
		if service != "" && c.Labels[models.LabelComposeService] != service {
			continue
		}
		out = append(out, c)
	}
	return out
}

func imageRefs(cs []container.Summary) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range cs {
		if c.Image != "" && !seen[c.Image] {
			seen[c.Image] = true
			out = append(out, c.Image)
		}
	}
	return out
}

// composeUpdate runs the compose steps with guaranteed dependent restore and
// lifecycle events on a fresh context.
func (cr *cmdRun) composeUpdate(api dockerAPI, spec composeSpec) (res commandExecResult, refs []string) {
	cr.Event(spec.label(), "", "update_started")
	st := &depState{}
	defer func() {
		res = cr.finishDependents(st, res)
		cr.Event(spec.label(), "", lifecycleAction(res))
	}()
	res, refs = cr.composeCore(api, spec, st)
	return res, refs
}

func (cr *cmdRun) composeCore(api dockerAPI, spec composeSpec, st *depState) (commandExecResult, []string) {
	a := cr.a
	var services []string
	if spec.Service != "" {
		if !composeServiceRe.MatchString(spec.Service) {
			return precheckFailure(fmt.Sprintf("invalid compose service name %q", spec.Service)), nil
		}
		services = []string{spec.Service}
	}

	pctx, cancel := context.WithTimeout(cr.ctx, models.DockerPrecheckBudget)
	all, err := api.ContainerList(pctx, container.ListOptions{All: true})
	cancel()
	if err != nil {
		return precheckFailure(fmt.Sprintf("list containers: %s", err)), nil
	}
	scope := projectContainers(all, spec.WorkDir, spec.Service)
	refs := imageRefs(scope)
	wasRunning := map[string]bool{}
	for _, c := range scope {
		if c.State == "running" {
			wasRunning[c.Labels[models.LabelComposeService]] = true
		}
	}
	var runningDeps []depRecord
	for _, d := range findDependents(all, scope) {
		if d.Running {
			runningDeps = append(runningDeps, d)
		}
	}

	// Sanitized copy: removed on every exit path, including panics (SEC C-5).
	sanitized := ""
	defer func() { removeSanitized(sanitized) }()

	// Pull, with the sanitize retry sharing the same 15m budget.
	cr.Step(models.StepPull)
	pullStart := a.clock().Now()
	pullArgs, err := composeArgs(spec.WorkDir, "", []string{"pull"}, services...)
	if err != nil {
		return precheckFailure(err.Error()), refs
	}
	out, timedOut, err := cr.runCompose(pullArgs, models.ComposePullLimit)
	if err != nil && !timedOut && isComposeValidationError(string(out)) {
		slog.Info("docker_update: compose validation failed, retrying with sanitized file",
			"stack", spec.label(), "error", string(out))
		path := filepath.Join(spec.WorkDir, sanitizedComposeName)
		if merr := cr.markDocker(spec.markerKey(), func(r *dockerRecord) { r.SanitizedFile = path }); merr != nil {
			return persistenceFailure(models.StepPull, "sanitize compose file", merr), refs
		}
		sf, serr := sanitizeComposeFile(spec.WorkDir)
		if serr != nil {
			return commandExecResult{
				Status:  models.ResultStatusError,
				Step:    models.StepPull,
				Message: fmt.Sprintf("compose pull failed: %s\nsanitize fallback also failed: %s", err, serr),
				Stderr:  truncateOutput(out, models.MaxStderrTailBytes),
			}, refs
		}
		sanitized = sf
		retryArgs, aerr := composeArgs(spec.WorkDir, sanitized, []string{"pull"}, services...)
		if aerr != nil {
			return precheckFailure(aerr.Error()), refs
		}
		remaining := models.ComposePullLimit - a.clock().Now().Sub(pullStart)
		if remaining < time.Second {
			remaining = time.Second
		}
		out, timedOut, err = cr.runCompose(retryArgs, remaining)
	}
	if timedOut {
		return timeoutFailure(models.StepPull, models.ComposePullLimit, out), refs
	}
	if err != nil {
		return commandExecResult{
			Status:  models.ResultStatusError,
			Step:    models.StepPull,
			Message: fmt.Sprintf("compose pull failed: %s\n%s", err, truncateOutput(out, 4000)),
			Stderr:  truncateOutput(out, models.MaxStderrTailBytes),
		}, refs
	}
	pullOut := out

	if r := cr.stopDependents(api, st, spec.markerKey(), runningDeps); r != nil {
		return *r, refs
	}

	// Recreate.
	cr.Step(models.StepRecreate)
	upArgs, err := composeArgs(spec.WorkDir, sanitized, []string{"up", "-d"}, services...)
	if err != nil {
		return precheckFailure(err.Error()), refs
	}
	out, timedOut, err = cr.runCompose(upArgs, models.ComposeUpLimit)
	if timedOut {
		return timeoutFailure(models.StepRecreate, models.ComposeUpLimit, out), refs
	}
	if err != nil {
		return commandExecResult{
			Status:  models.ResultStatusError,
			Step:    models.StepRecreate,
			Message: fmt.Sprintf("compose up failed: %s\n%s", err, truncateOutput(append(append([]byte(nil), pullOut...), out...), 4000)),
			Stderr:  truncateOutput(out, models.MaxStderrTailBytes),
		}, refs
	}
	combined := append(append([]byte(nil), pullOut...), out...)

	// Verify: services that were running are running, and not restarting, after
	// the verification window. Compose has no rollback.
	cr.Verifying(models.StepVerify)
	vctx, vcancel := context.WithTimeout(cr.ctx, models.VerifyBudget)
	defer vcancel()
	if !a.pauseFor(vctx, models.VerifyWindow) {
		return commandExecResult{Status: models.ResultStatusError, Step: models.StepVerify,
			Message: "verification interrupted before the verification window elapsed"}, refs
	}
	after, err := api.ContainerList(vctx, container.ListOptions{All: true})
	if err != nil {
		return commandExecResult{Status: models.ResultStatusError, Step: models.StepVerify,
			Message: fmt.Sprintf("verification failed: list containers: %s", err)}, refs
	}
	postScope := projectContainers(after, spec.WorkDir, spec.Service)
	refs = append(refs, imageRefs(postScope)...)
	for svc := range wasRunning {
		ok := false
		for _, c := range postScope {
			if c.Labels[models.LabelComposeService] == svc && c.State == "running" {
				ok = true
				break
			}
		}
		if !ok {
			return commandExecResult{
				Status:  models.ResultStatusError,
				Step:    models.StepVerify,
				Message: fmt.Sprintf("service %s is not running %s after the update", svc, models.FormatDurationShort(models.VerifyWindow)),
				Output:  string(combined),
			}, refs
		}
	}

	label := "stack " + filepath.Base(spec.WorkDir)
	if spec.Service != "" {
		label = "service " + spec.Service
	}
	return commandExecResult{
		Status:  models.ResultStatusSuccess,
		Message: fmt.Sprintf("updated compose %s\n%s", label, truncateOutput(combined, 4000)),
	}, refs
}

func timeoutFailure(step string, limit time.Duration, out []byte) commandExecResult {
	return commandExecResult{
		Status:  models.ResultStatusError,
		Step:    step,
		Reason:  models.ReasonTimeout,
		Message: "timed out after " + models.FormatDurationShort(limit),
		Stderr:  truncateOutput(out, models.MaxStderrTailBytes),
	}
}

// runCompose runs one docker compose invocation under its own limit with the
// SIGTERM-then-SIGKILL stop (FR-043, FR-044).
func (cr *cmdRun) runCompose(args []string, limit time.Duration) ([]byte, bool, error) {
	return runLimited(cr.ctx, cr.a.newProc("docker", args...), limit, models.ChildGrace, cr.a.clock())
}

// ---------------------------------------------------------------------------
// Standalone
// ---------------------------------------------------------------------------

// standaloneUpdate wraps the standalone steps with the same deferred dependent
// restore and lifecycle events as the compose path.
func (cr *cmdRun) standaloneUpdate(api dockerAPI, insp container.InspectResponse) (res commandExecResult, refs []string) {
	name := strings.TrimPrefix(insp.Name, "/")
	imageRef := ""
	if insp.Config != nil {
		imageRef = insp.Config.Image
	}
	cr.Event(name, imageRef, "update_started")
	st := &depState{}
	defer func() {
		res = cr.finishDependents(st, res)
		cr.Event(name, imageRef, lifecycleAction(res))
	}()
	res = cr.standaloneCore(api, insp, st)
	if imageRef != "" {
		refs = []string{imageRef}
	}
	return res, refs
}

// consumePullStream reads the whole pull stream. A JSON entry carrying `error`
// or `errorDetail` is a registry failure even though the HTTP call succeeded
// (FR-034).
func consumePullStream(r io.Reader) (string, error) {
	dec := json.NewDecoder(r)
	for {
		var m struct {
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				return "", nil
			}
			return "", err
		}
		if m.ErrorDetail.Message != "" {
			return m.ErrorDetail.Message, nil
		}
		if m.Error != "" {
			return m.Error, nil
		}
	}
}

func (cr *cmdRun) standaloneCore(api dockerAPI, insp container.InspectResponse, st *depState) commandExecResult {
	a := cr.a
	name := strings.TrimPrefix(insp.Name, "/")
	oldID := insp.ID
	imageRef := insp.Config.Image
	wasRunning := insp.State != nil && insp.State.Running
	setAside := fmt.Sprintf("%s.riot-old-%s", name, cr.id[:8])

	// Precheck: a set-aside container from an earlier failed rollback blocks the
	// update and is never deleted (FR-042).
	cr.Step(models.StepPrecheck)
	pctx, pcancel := context.WithTimeout(cr.ctx, models.DockerPrecheckBudget)
	all, err := api.ContainerList(pctx, container.ListOptions{All: true})
	pcancel()
	if err != nil {
		return precheckFailure(fmt.Sprintf("list containers: %s", err))
	}
	for _, c := range all {
		if n := summaryName(c); strings.HasPrefix(n, name+".riot-old-") {
			return precheckFailure(fmt.Sprintf("set-aside container %s from an earlier failed update still exists; remove or rename it before updating %s", n, name))
		}
	}
	self, _ := findByName(all, name)
	var runningDeps []depRecord
	for _, d := range findDependents(all, []container.Summary{self}) {
		if d.Running {
			runningDeps = append(runningDeps, d)
		}
	}

	// Pull before touching anything (a failed pull leaves the container alone).
	cr.Step(models.StepPull)
	pullCtx, pullCancel := context.WithTimeout(cr.ctx, models.StandalonePullLimit)
	defer pullCancel()
	stream, err := api.ImagePull(pullCtx, imageRef, image.PullOptions{})
	if err != nil {
		return pullFailure(pullCtx, cr.ctx, fmt.Sprintf("image pull failed: %s", err), err.Error())
	}
	pullMsg, err := consumePullStream(stream)
	stream.Close()
	if err != nil {
		return pullFailure(pullCtx, cr.ctx, fmt.Sprintf("image pull failed: %s", err), err.Error())
	}
	if pullMsg != "" {
		return commandExecResult{
			Status:  models.ResultStatusError,
			Step:    models.StepPull,
			Message: "image pull failed: " + pullMsg,
			Stderr:  pullMsg,
		}
	}
	newImg, err := api.ImageInspect(pullCtx, imageRef)
	if err != nil {
		return pullFailure(pullCtx, cr.ctx, fmt.Sprintf("inspect pulled image: %s", err), err.Error())
	}
	if newImg.ID == insp.Image {
		return commandExecResult{
			Status:  models.ResultStatusSuccess,
			Message: fmt.Sprintf("container %s (%s) already up to date", name, imageRef),
		}
	}

	if r := cr.stopDependents(api, st, name, runningDeps); r != nil {
		return *r
	}

	// Recreate: stop, rename aside, create the replacement, start it. Every
	// mutation is preceded by a durable marker write (SEC C-4a).
	cr.Step(models.StepRecreate)
	rctx, rcancel := context.WithTimeout(cr.ctx, models.StandaloneRecreateBudget)
	defer rcancel()
	if err := cr.markDocker(name, func(r *dockerRecord) {
		r.OriginalName, r.OldID, r.SetAsideName, r.WasRunning = name, oldID, setAside, wasRunning
	}); err != nil {
		return persistenceFailure(models.StepRecreate, "recreate "+name, err)
	}
	rb := rollbackState{name: name, oldID: oldID, setAside: setAside, wasRunning: wasRunning}

	if wasRunning {
		timeout := 30
		if err := api.ContainerStop(rctx, oldID, container.StopOptions{Timeout: &timeout}); err != nil {
			return cr.rollback(api, rb, models.StepRecreate, fmt.Sprintf("stop failed: %s", err), models.ReasonRolledBack)
		}
	}
	if err := api.ContainerRename(rctx, oldID, setAside); err != nil {
		return cr.rollback(api, rb, models.StepRecreate, fmt.Sprintf("rename failed: %s", err), models.ReasonRolledBack)
	}
	rb.renamed = true

	newContainer, err := api.ContainerCreate(rctx, insp.Config, insp.HostConfig, networkConfigFrom(insp), name)
	if err != nil {
		return cr.rollback(api, rb, models.StepRecreate, fmt.Sprintf("create failed: %s", err), models.ReasonRolledBack)
	}
	rb.replacementID = newContainer.ID
	if err := cr.markDocker(name, func(r *dockerRecord) { r.ReplacementID = newContainer.ID }); err != nil {
		res := cr.rollback(api, rb, models.StepRecreate, fmt.Sprintf("marker write failed: %s", err), models.ReasonPersistence)
		return res
	}
	if wasRunning {
		if err := api.ContainerStart(rctx, newContainer.ID, container.StartOptions{}); err != nil {
			return cr.rollback(api, rb, models.StepRecreate, fmt.Sprintf("start failed: %s", err), models.ReasonRolledBack)
		}

		// Verify: running and not restarting after the verification window.
		cr.Verifying(models.StepVerify)
		vctx, vcancel := context.WithTimeout(cr.ctx, models.VerifyBudget)
		defer vcancel()
		if !a.pauseFor(vctx, models.VerifyWindow) {
			return cr.rollback(api, rb, models.StepVerify, "verification interrupted", models.ReasonRolledBack)
		}
		ni, err := api.ContainerInspect(vctx, newContainer.ID)
		if err != nil {
			return cr.rollback(api, rb, models.StepVerify, fmt.Sprintf("inspect replacement: %s", err), models.ReasonRolledBack)
		}
		if ni.State == nil || !ni.State.Running || ni.State.Restarting {
			state := "not running"
			if ni.State != nil && ni.State.Restarting {
				state = "restarting"
			}
			return cr.rollback(api, rb, models.StepVerify,
				fmt.Sprintf("replacement is %s %s after start", state, models.FormatDurationShort(models.VerifyWindow)), models.ReasonRolledBack)
		}
	}

	// Cleanup: the only place the old container is removed, by its ID, and only
	// after verification passed (NFR-004, SEC C-9).
	cr.Step(models.StepCleanup)
	cctx, ccancel := context.WithTimeout(context.Background(), models.CleanupBudget)
	defer ccancel()
	if err := api.ContainerRemove(cctx, oldID, container.RemoveOptions{}); err != nil {
		slog.Warn("docker_update: could not remove the previous container", "container", setAside, "id", oldID, "error", err)
		return commandExecResult{
			Status: models.ResultStatusSuccess,
			Message: fmt.Sprintf("updated container %s (%s); previous container %s was not removed: %s",
				name, imageRef, setAside, err),
		}
	}
	if err := cr.markDocker(name, func(r *dockerRecord) {
		r.OldID, r.SetAsideName, r.ReplacementID = "", "", ""
	}); err != nil {
		slog.Warn("docker_update: could not clear the recreate marker", "error", err)
	}
	return commandExecResult{
		Status:  models.ResultStatusSuccess,
		Message: fmt.Sprintf("updated container %s (%s)", name, imageRef),
	}
}

// pullFailure reports a failed pull, as a timeout when the pull limit (and not
// the whole command) ended it.
func pullFailure(pullCtx, cmdCtx context.Context, msg, stderr string) commandExecResult {
	if errors.Is(pullCtx.Err(), context.DeadlineExceeded) && cmdCtx.Err() == nil {
		return commandExecResult{
			Status:  models.ResultStatusError,
			Step:    models.StepPull,
			Reason:  models.ReasonTimeout,
			Message: "timed out after " + models.FormatDurationShort(models.StandalonePullLimit),
			Stderr:  stderr,
		}
	}
	return commandExecResult{Status: models.ResultStatusError, Step: models.StepPull, Message: msg, Stderr: stderr}
}

// networkConfigFrom rebuilds the replacement's per-network settings from the old
// container so it keeps its static address, aliases, MAC and links (FR-037). Only
// configuration is copied; runtime-assigned fields (EndpointID, IPAddress,
// Gateway) are left for Docker to assign to the new endpoint.
func networkConfigFrom(insp container.InspectResponse) *network.NetworkingConfig {
	if insp.NetworkSettings == nil || len(insp.NetworkSettings.Networks) == 0 {
		return nil
	}
	cfg := &network.NetworkingConfig{EndpointsConfig: make(map[string]*network.EndpointSettings)}
	for netName, s := range insp.NetworkSettings.Networks {
		cfg.EndpointsConfig[netName] = &network.EndpointSettings{
			IPAMConfig: s.IPAMConfig,
			Links:      s.Links,
			Aliases:    s.Aliases,
			MacAddress: s.MacAddress,
			DriverOpts: s.DriverOpts,
			NetworkID:  s.NetworkID,
			DNSNames:   s.DNSNames,
		}
	}
	return cfg
}

// rollbackState is what a rollback needs to know about the partial recreate.
type rollbackState struct {
	name          string
	oldID         string
	setAside      string
	wasRunning    bool
	renamed       bool
	replacementID string
}

// rollback undoes a failed recreate on a fresh 60s context: remove the
// replacement, give the old container its name back, return it to its prior run
// state (FR-039). The old container is never removed. If the rollback itself
// fails the command ends at `rollback` naming the set-aside container (FR-041).
func (cr *cmdRun) rollback(api dockerAPI, rb rollbackState, failedStep, cause, reason string) commandExecResult {
	slog.Warn("docker_update: rolling back to previous container", "container", rb.name, "step", failedStep, "cause", cause)
	ctx, cancel := context.WithTimeout(context.Background(), models.RollbackBudget)
	defer cancel()

	fail := func(what string, err error) commandExecResult {
		slog.Error("docker_update: rollback failed", "container", rb.name, "set_aside", rb.setAside, "step", what, "error", err)
		return commandExecResult{
			Status: models.ResultStatusError,
			Step:   models.StepRollback,
			Message: fmt.Sprintf("rollback failed (%s: %s) after: %s; previous container preserved as %s (id %s)",
				what, err, cause, rb.setAside, rb.oldID),
			Stderr: err.Error(),
		}
	}

	cr.Step(models.StepRollback)
	if rb.replacementID != "" {
		if err := api.ContainerRemove(ctx, rb.replacementID, container.RemoveOptions{Force: true}); err != nil {
			return fail("remove replacement", err)
		}
	}
	if rb.renamed {
		if err := api.ContainerRename(ctx, rb.oldID, rb.name); err != nil {
			return fail("rename back", err)
		}
	}
	if rb.wasRunning {
		if err := api.ContainerStart(ctx, rb.oldID, container.StartOptions{}); err != nil {
			return fail("restart previous container", err)
		}
	}
	if err := cr.markDocker(rb.name, func(r *dockerRecord) {
		r.OldID, r.SetAsideName, r.ReplacementID = "", "", ""
	}); err != nil {
		slog.Warn("docker_update: could not clear the recreate marker after rollback", "error", err)
	}
	return commandExecResult{
		Status:  models.ResultStatusError,
		Step:    failedStep,
		Reason:  reason,
		Message: "rolled back to previous container: " + cause,
		Stderr:  cause,
	}
}

// ---------------------------------------------------------------------------
// Bulk
// ---------------------------------------------------------------------------

// dockerBulkUpdate updates a list of containers: children before the parents
// whose network namespace they use. The union of the targets is locked up
// front, each inner update runs with the locks held, and the wall-time budget is
// checked before every container (AD-013).
func (a *Agent) dockerBulkUpdate(cr *cmdRun) commandExecResult {
	rawIDs, ok := cr.payload.Params["container_ids"].([]interface{})
	if !ok || len(rawIDs) == 0 {
		return precheckFailure("container_ids is required")
	}
	api, err := a.newDocker()
	if err != nil {
		return precheckFailure(fmt.Sprintf("docker client: %s", err))
	}
	defer api.Close()

	cr.Step(models.StepPrecheck)
	if r, bad := cr.restartInProgress(); bad {
		return r
	}

	type node struct {
		id, name string
		summary  container.Summary
	}
	pctx, pcancel := context.WithTimeout(cr.ctx, models.DockerPrecheckBudget)
	all, err := api.ContainerList(pctx, container.ListOptions{All: true})
	pcancel()
	if err != nil {
		return precheckFailure(fmt.Sprintf("list containers: %s", err))
	}
	var nodes []node
	var keys []string
	for _, raw := range rawIDs {
		cid, _ := raw.(string)
		if cid == "" {
			continue
		}
		var found *container.Summary
		for i := range all {
			if all[i].ID == cid {
				found = &all[i]
				break
			}
		}
		if found == nil {
			continue
		}
		nodes = append(nodes, node{id: cid, name: summaryName(*found), summary: *found})
		keys = append(keys, models.TargetKeyContainer(summaryName(*found)))
		if wd := found.Labels[models.LabelComposeWorkingDir]; wd != "" &&
			found.Labels[models.LabelComposeProject] != "" && found.Labels[models.LabelComposeService] != "" {
			keys = append(keys, models.TargetKeyCompose(wd))
		}
	}
	if len(nodes) == 0 {
		return precheckFailure("none of the requested containers exist")
	}
	if key, holder, ok := cr.lock(models.NormalizeTargetKeys(keys...)...); !ok {
		return conflictResult(key, holder)
	}

	// Children (and independent containers) first, then the parents others use.
	var first, parents []node
	for _, n := range nodes {
		isParent := false
		for _, other := range nodes {
			if other.id != n.id && networkDependsOn(other.summary, n.summary) {
				isParent = true
				break
			}
		}
		if isParent {
			parents = append(parents, n)
		} else {
			first = append(first, n)
		}
	}
	ordered := append(first, parents...)

	budget := models.BulkAgentBudget(len(rawIDs))
	var lines []string
	var refs []string
	failed := 0
	firstFailStep := ""
	for i, n := range ordered {
		if a.clock().Now().Sub(cr.start)+models.BulkContainerWorstCase > budget {
			for _, rest := range ordered[i:] {
				lines = append(lines, fmt.Sprintf("%s: %s", rest.name, models.BulkSkippedMessage))
				failed++
			}
			if firstFailStep == "" {
				firstFailStep = models.StepPrecheck
			}
			break
		}
		res, r := cr.dockerUpdateOne(api, updateTarget{Name: n.name, ID: n.id}, updateOpts{lockHeld: true})
		refs = append(refs, r...)
		lines = append(lines, fmt.Sprintf("%s: %s", n.name, res.Message))
		if res.Status == models.ResultStatusError {
			failed++
			if firstFailStep == "" {
				firstFailStep = res.Step
			}
		}
		cr.Percent((i + 1) * 100 / len(ordered))
	}

	a.invalidateFreshness(refs)
	a.triggerTelemetry()

	summary := fmt.Sprintf("bulk update: %d/%d succeeded\n%s", len(ordered)-failed, len(ordered), strings.Join(lines, "\n"))
	if failed > 0 {
		return commandExecResult{Status: models.ResultStatusError, Step: firstFailStep, Message: summary}
	}
	return commandExecResult{Status: models.ResultStatusSuccess, Message: summary}
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// triggerTelemetry signals the telemetry loop to send immediately so the
// dashboard reflects updated container state without waiting for the next cycle.
func (a *Agent) triggerTelemetry() {
	select {
	case a.telemetryNow <- struct{}{}:
	default:
		// Already pending — no need to queue another
	}
}

// freshnessInvalidator is the image-freshness cache as the update paths see it.
type freshnessInvalidator interface {
	InvalidateFreshness(refs ...string)
	ClearFreshnessCache()
}

// freshnessCache returns the injected cache or the Docker collector's.
func (a *Agent) freshnessCache() freshnessInvalidator {
	if a.freshness != nil {
		return a.freshness
	}
	if a.registry == nil {
		return nil
	}
	for _, c := range a.registry.Collectors() {
		if dc, ok := c.(*collectors.DockerCollector); ok {
			return dc
		}
	}
	return nil
}

// invalidateFreshness drops only the cache entries for the image references an
// update involved, in memory and on disk (FR-052).
func (a *Agent) invalidateFreshness(refs []string) {
	if len(refs) == 0 {
		return
	}
	if fc := a.freshnessCache(); fc != nil {
		fc.InvalidateFreshness(refs...)
	}
}

// clearFreshnessCache clears the whole image freshness cache (the explicit
// docker_check_updates action).
func (a *Agent) clearFreshnessCache() {
	if fc := a.freshnessCache(); fc != nil {
		fc.ClearFreshnessCache()
	}
}

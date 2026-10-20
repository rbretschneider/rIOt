package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/docker/docker/api/types/container"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
)

type stackPaths struct {
	WorkingDir string    `json:"working_dir"`
	Files      []string  `json:"config_files"`
	LastSeen   time.Time `json:"-"`
}

type stackRegistryRecord struct {
	Agreed   stackPaths `json:"agreed"`
	LastSeen time.Time  `json:"last_seen"`
}
type stackRegistryDocument struct {
	Version int                            `json:"version"`
	Stacks  map[string]stackRegistryRecord `json:"stacks"`
}

const stackRegistryMaxBytes = 4 << 20

// Both methods run under stacks.mu; no registry input or rewrite is unbounded.
func (a *Agent) loadStackRegistry() error {
	r := &a.stacks
	if r.loaded {
		return nil
	}
	r.entries = map[string]stackPaths{}
	raw, err := osReadFile(a.stackRegistryFile().path, stackRegistryMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		r.loaded = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read stack registry")
	}
	var doc stackRegistryDocument
	if json.Unmarshal(raw, &doc) != nil || doc.Version != 2 || len(doc.Stacks) > 500 {
		return fmt.Errorf("invalid stack registry")
	}
	for project, record := range doc.Stacks {
		if !composeProjectRe.MatchString(project) || len(record.Agreed.Files) > 10 {
			return fmt.Errorf("invalid stack registry entry")
		}
		entry := record.Agreed
		entry.LastSeen = record.LastSeen
		r.entries[project] = entry
	}
	r.loaded = true
	return nil
}
func (a *Agent) saveStackRegistry() error {
	doc := stackRegistryDocument{Version: 2, Stacks: map[string]stackRegistryRecord{}}
	for project, entry := range a.stacks.entries {
		doc.Stacks[project] = stackRegistryRecord{Agreed: entry, LastSeen: entry.LastSeen}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if len(raw) > stackRegistryMaxBytes {
		return fmt.Errorf("stack registry byte cap exceeded")
	}
	return a.stackRegistryFile().SaveBytes(raw)
}

type agentStackRegistry struct {
	mu      sync.Mutex
	loaded  bool
	entries map[string]stackPaths
	path    string
}

func (a *Agent) stackRegistryFile() *atomicJSONFile {
	path := a.stacks.path
	if path == "" {
		path = filepath.Join(filepath.Dir(SeenCommandsPath()), "docker-stacks.json")
	}
	return newAtomicJSONFile(path, osStateFS{})
}
func (a *Agent) resolveStackPaths(snap daemonSnapshot, project, dir string, files []string, ack bool) (daemonSnapshot, trustedDir, error) {
	r := &a.stacks
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := a.loadStackRegistry(); err != nil {
		return snap, trustedDir{}, err
	}
	t := a.composeTrust()
	for i, file := range files {
		if resolved, err := t.fs.EvalSymlinks(file); err == nil {
			files[i] = resolved
		} else {
			return snap, trustedDir{}, fmt.Errorf("cannot resolve compose file")
		}
	}
	snap = t.bind(snap)
	var observed *stackPaths
	members := 0
	for _, c := range snap.Containers {
		if c.Labels[models.LabelComposeProject] != project {
			continue
		}
		members++
		if !snap.isSource(c) || c.Labels[models.LabelComposeService] == "" || c.Labels["com.docker.compose.version"] == "" {
			return snap, trustedDir{}, fmt.Errorf("untrusted stack provenance")
		}
		wd := c.Labels[models.LabelComposeWorkingDir]
		if wd == "" {
			return snap, trustedDir{}, fmt.Errorf("stack paths conflict")
		}
		candidate := stackPaths{WorkingDir: wd}
		for _, f := range strings.Split(c.Labels["com.docker.compose.project.config_files"], ",") {
			if f != "" {
				if !filepath.IsAbs(f) {
					f = filepath.Join(wd, f)
				}
				if len(f) > 4096 || len(candidate.Files) >= 10 {
					return snap, trustedDir{}, fmt.Errorf("compose path bound exceeded")
				}
				resolved, err := t.fs.EvalSymlinks(filepath.Clean(f))
				if err != nil {
					return snap, trustedDir{}, fmt.Errorf("cannot resolve compose file")
				}
				candidate.Files = append(candidate.Files, resolved)
			}
		}
		if observed != nil && (snap.key(observed.WorkingDir) != snap.key(wd) || !reflect.DeepEqual(observed.Files, candidate.Files)) {
			return snap, trustedDir{}, fmt.Errorf("stack paths conflict")
		}
		observed = &candidate
	}
	agreed, known := r.entries[project]
	if observed == nil {
		if !known || ack {
			return snap, trustedDir{}, fmt.Errorf("stack not found")
		}
		observed = &agreed
		snap.Containers = append(append([]container.Summary(nil), snap.Containers...), container.Summary{ID: "registry:" + project, Labels: map[string]string{models.LabelComposeProject: project, models.LabelComposeWorkingDir: agreed.WorkingDir, models.LabelComposeService: "registry"}})
	}
	if dir != "" && snap.key(dir) != snap.key(observed.WorkingDir) {
		return snap, trustedDir{}, fmt.Errorf("paths_changed")
	}
	if len(files) > 0 && len(observed.Files) > 0 && !reflect.DeepEqual(files, observed.Files) {
		return snap, trustedDir{}, fmt.Errorf("paths_changed")
	}
	if known && !ack && (snap.key(agreed.WorkingDir) != snap.key(observed.WorkingDir) || (len(observed.Files) > 0 && !reflect.DeepEqual(agreed.Files, observed.Files))) {
		return snap, trustedDir{}, fmt.Errorf("paths_changed")
	}
	if err := a.checkStackOuterBound(observed.WorkingDir); err != nil {
		return snap, trustedDir{}, err
	}
	if err := t.strictWritable(snap, observed.WorkingDir); err != nil {
		return snap, trustedDir{}, err
	}
	td, err := t.Check(snap, trustRequest{Dir: observed.WorkingDir, Project: project, Files: observed.Files})
	if err != nil {
		return snap, trustedDir{}, err
	}
	if len(files) > 0 && !reflect.DeepEqual(files, td.Files()) {
		return snap, trustedDir{}, fmt.Errorf("paths_changed")
	}
	next := stackPaths{WorkingDir: td.Path(), Files: td.Files(), LastSeen: time.Now().UTC()}
	if known && !ack && (snap.key(agreed.WorkingDir) != snap.key(next.WorkingDir) || !reflect.DeepEqual(agreed.Files, next.Files)) {
		return snap, trustedDir{}, fmt.Errorf("paths_changed")
	}
	if !known || ack || time.Since(agreed.LastSeen) > 24*time.Hour {
		if !known && len(r.entries) >= 500 {
			return snap, trustedDir{}, fmt.Errorf("stack registry full")
		}
		old := r.entries[project]
		r.entries[project] = next
		if err := a.saveStackRegistry(); err != nil {
			if known {
				r.entries[project] = old
			} else {
				delete(r.entries, project)
			}
			return snap, trustedDir{}, fmt.Errorf("cannot persist stack registry")
		}
	}
	_ = members
	return snap, td, nil
}
func (a *Agent) checkStackOuterBound(dir string) error {
	fs := a.composeTrust().fs
	resolved, err := fs.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("cannot resolve stack directory")
	}
	if resolved == string(filepath.Separator) {
		return fmt.Errorf("denied stack directory")
	}
	denied := []string{"/etc", "/root", "/run", "/var/run", "/var/lib/docker", "/proc", "/sys", "/dev", "/boot", "/var/lib/riot", "/usr/local/lib/riot", filepath.Dir(ConfigPath()), filepath.Dir(ServerCertPath())}
	for _, path := range []string{a.config.Server.ClientCert, a.config.Server.ClientKey, a.config.Server.CACertFile} {
		if path != "" {
			denied = append(denied, filepath.Dir(path))
		}
	}
	for _, root := range denied {
		if !filepath.IsAbs(root) {
			continue
		}
		if r, e := fs.EvalSymlinks(root); e == nil {
			root = r
		}
		if within(root, resolved, false) || within(resolved, root, false) {
			return fmt.Errorf("denied stack directory")
		}
	}
	for _, part := range strings.Split(filepath.ToSlash(resolved), "/") {
		switch part {
		case ".ssh", ".docker", ".kube", ".gnupg", ".config":
			return fmt.Errorf("denied stack directory")
		}
	}
	return nil
}

// observeStackRegistry remembers source-validated paths before a stack is taken
// down outside rIOt. Existing entries are refreshed at most once each day.
func (a *Agent) observeStackRegistry(ctx context.Context, info *models.DockerInfo) {
	if info == nil || !info.Available {
		return
	}
	a.stacks.mu.Lock()
	if err := a.loadStackRegistry(); err != nil {
		a.stacks.mu.Unlock()
		return
	}
	changed := false
	for project, entry := range a.stacks.entries {
		if !entry.LastSeen.IsZero() && time.Since(entry.LastSeen) > 90*24*time.Hour {
			delete(a.stacks.entries, project)
			changed = true
		}
	}
	if changed {
		a.saveStackRegistry()
	}
	a.stacks.mu.Unlock()
	projects := map[string]bool{}
	for _, ctr := range info.Containers {
		project := ctr.Labels[models.LabelComposeProject]
		if composeProjectRe.MatchString(project) {
			projects[project] = true
		}
	}
	if len(projects) == 0 {
		return
	}
	api, err := a.newDocker()
	if err != nil {
		return
	}
	defer api.Close()
	snap, err := loadDaemonSnapshot(ctx, api)
	if err != nil {
		return
	}
	for project := range projects {
		if ctx.Err() != nil {
			return
		}
		a.stacks.mu.Lock()
		entry, known := a.stacks.entries[project]
		skip := known && time.Since(entry.LastSeen) < 24*time.Hour
		a.stacks.mu.Unlock()
		if skip {
			continue
		}
		a.resolveStackPaths(snap, project, "", nil, false)
	}
}

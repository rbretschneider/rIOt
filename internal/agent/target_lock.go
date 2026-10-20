package agent

import (
	"sort"
	"strings"
	"sync"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// lockHolder identifies the command that owns a target lock.
type lockHolder struct {
	CmdID  string
	Action string
}

// targetLocks is the agent's in-flight lock table (FR-054, AD-012). The keys
// are the frozen target keys of models: "host", "docker:compose:<dir>",
// "docker:container:<name>". Acquisition never blocks; a conflict is reported
// to the command, which then fails at precheck without touching Docker.
type targetLocks struct {
	mu sync.Mutex
	m  map[string]lockHolder
}

func newTargetLocks() *targetLocks {
	return &targetLocks{m: map[string]lockHolder{}}
}

// TryLockAll takes every key for the command, all or nothing. Keys the same
// command already holds count as acquired, so a bulk update can pre-lock its
// union and let the inner updates run with the locks held. On conflict nothing
// is acquired and the first conflicting key (in sorted order) and its holder
// are returned.
func (l *targetLocks) TryLockAll(keys []string, cmdID, action string) (conflictKey string, holder lockHolder, ok bool) {
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)

	l.mu.Lock()
	defer l.mu.Unlock()
	docker, exclusive := false, false
	for _, key := range sorted {
		if strings.HasPrefix(key, models.TargetKeyDockerPrefix) {
			docker = true
		}
		if key == "docker:host" {
			exclusive = true
		}
	}
	restarting := action == models.ActionAgentUpdate || action == models.ActionAgentUninstall
	for key, h := range l.m {
		if h.CmdID == cmdID {
			continue
		}
		heldDocker := strings.HasPrefix(key, models.TargetKeyDockerPrefix)
		heldRestart := key == models.TargetKeyHost && (h.Action == models.ActionAgentUpdate || h.Action == models.ActionAgentUninstall)
		if docker && (key == "docker:host" || heldRestart) || exclusive && heldDocker || restarting && heldDocker {
			return key, h, false
		}
	}
	for _, k := range sorted {
		if h, held := l.m[k]; held && h.CmdID != cmdID {
			return k, h, false
		}
	}
	for _, k := range sorted {
		l.m[k] = lockHolder{CmdID: cmdID, Action: action}
	}
	return "", lockHolder{}, true
}

// Unlock releases the keys held by cmdID. Keys held by another command are left
// alone, so a late release can never free someone else's lock (FR-056).
func (l *targetLocks) Unlock(keys []string, cmdID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		if h, held := l.m[k]; held && h.CmdID == cmdID {
			delete(l.m, k)
		}
	}
}

// Holder returns the owner of key.
func (l *targetLocks) Holder(key string) (lockHolder, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	h, ok := l.m[key]
	return h, ok
}

// HeldWithPrefix reports whether any held key starts with prefix (SEC C-4c:
// "docker:" blocks agent_update and agent_uninstall). With several matches the
// smallest key is returned so messages are deterministic.
func (l *targetLocks) HeldWithPrefix(prefix string) (key string, holder lockHolder, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, h := range l.m {
		if strings.HasPrefix(k, prefix) && (!ok || k < key) {
			key, holder, ok = k, h, true
		}
	}
	return key, holder, ok
}

// lockLabel turns a target key into the name shown in conflict messages.
func lockLabel(key string) string {
	switch {
	case strings.HasPrefix(key, "docker:container:"):
		return strings.TrimPrefix(key, "docker:container:")
	case strings.HasPrefix(key, "docker:compose:"):
		return strings.TrimPrefix(key, "docker:compose:")
	case strings.HasPrefix(key, "docker:id:"):
		return strings.TrimPrefix(key, "docker:id:")
	default:
		return key
	}
}

// conflictMessage is the FR-055 text for a command refused because key is held.
func conflictMessage(key string, holder lockHolder) string {
	return "update already in progress for " + lockLabel(key) + " (command " + holder.CmdID + ")"
}

// hostKeyFor returns the keys a host-scoped action locks, or nil. agent_uninstall
// is not a server-side host action but must be visible to the Docker
// exclusion check, so it takes the host key on the agent.
func hostKeyFor(action string) []string {
	if keys := models.HostTargetKeys(action); keys != nil {
		return keys
	}
	if action == models.ActionAgentUninstall {
		return []string{models.TargetKeyHost}
	}
	return nil
}

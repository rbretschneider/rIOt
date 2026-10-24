package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Reboot-intent retention (PATCH-FLOW-001 AD-012). A command ID can reach the
// reboot decision again only inside the redelivery windows (server: about 70
// minutes; agent seen-ID store: 48 hours), so intents younger than
// rebootIntentMinKeep are never dropped, not even by the cap.
const (
	rebootIntentMaxAge  = 30 * 24 * time.Hour // age prune
	rebootIntentMinKeep = 72 * time.Hour      // never evicted younger than this
	rebootIntentCap     = 256                 // hard backstop
	rebootIntentSkew    = 5 * time.Minute

	rebootIntentVersion = 2
	// rebootIntentMaxFileBytes bounds the read: 256 v2 entries are about 20 KiB,
	// so anything larger is corrupt and is never read whole (C-1c).
	rebootIntentMaxFileBytes = 64 << 10
	// rebootIntentAsideMaxBytes bounds the single set-aside copy of a corrupt
	// store (C-1b).
	rebootIntentAsideMaxBytes = 64 << 10
	rebootIntentAsideSuffix   = ".corrupt"
	rebootIntentFileName      = "patch-reboot-intents.json"
)

// rebootClaim is the outcome of claiming the single reboot allowed per command.
type rebootClaim int

const (
	claimOK rebootClaim = iota
	claimAlreadyClaimed
	claimPersistFailed
	claimStoreFull
)

// rebootIntentFile is the v2 on-disk format. The legacy v1 format is the bare
// object {"<uuid>": true}.
type rebootIntentFile struct {
	Version int               `json:"version"`
	Intents map[string]string `json:"intents"`
}

// claimPatchReboot records, durably and at most once per command ID, the intent
// to reboot after a patch run. Anything other than claimOK means no reboot.
func (a *Agent) claimPatchReboot(id string) rebootClaim {
	if !models.IsCanonicalUUID(id) {
		return claimPersistFailed
	}
	a.rebootIntentMu.Lock()
	defer a.rebootIntentMu.Unlock()
	now := a.clock().Now().UTC()
	path, fs := a.rebootIntentLocation()
	store := newAtomicJSONFile(path, fs)

	intents, ok := a.loadRebootIntents(path, fs, now)
	if !ok {
		return claimPersistFailed
	}
	pruned, removed := pruneRebootIntents(intents, now)
	if _, claimed := pruned[id]; claimed {
		a.saveIfPruned(store, pruned, removed)
		return claimAlreadyClaimed
	}
	room, made := evictForCap(pruned, now)
	if !made {
		slog.Warn("reboot intent store full; reboot suppressed", "entries", len(pruned))
		return claimStoreFull
	}
	if len(room) < len(pruned) {
		removed += len(pruned) - len(room)
	}
	room[id] = now
	if err := store.Save(encodeRebootIntents(room)); err != nil {
		slog.Warn("cannot persist reboot intent; reboot suppressed", "error", err)
		return claimPersistFailed
	}
	if removed > 0 {
		slog.Info("reboot intents pruned", "removed", removed, "remaining", len(room))
	}
	return claimOK
}

func (a *Agent) rebootIntentLocation() (string, stateFS) {
	path := a.rebootIntentPath
	var fs stateFS = osStateFS{}
	if a.inflight != nil {
		fs = a.inflight.file.fs
		if path == "" {
			path = filepath.Join(filepath.Dir(a.inflight.file.path), rebootIntentFileName)
		}
	}
	if path == "" {
		path = filepath.Join(filepath.Dir(OutboxPath()), rebootIntentFileName)
	}
	return path, fs
}

// saveIfPruned persists a pruned store when the claim itself is a no-op. A save
// failure is only logged: the claim result (already claimed) is unaffected.
func (a *Agent) saveIfPruned(store *atomicJSONFile, intents map[string]time.Time, removed int) {
	if removed == 0 {
		return
	}
	if err := store.Save(encodeRebootIntents(intents)); err != nil {
		slog.Warn("cannot persist pruned reboot intents", "error", err)
		return
	}
	slog.Info("reboot intents pruned", "removed", removed, "remaining", len(intents))
}

// loadRebootIntents returns the stored intents. ok is false when the store is
// unusable and must fail closed: corrupt content younger than
// rebootIntentMinKeep, or a set-aside that could not be written.
func (a *Agent) loadRebootIntents(path string, fs stateFS, now time.Time) (map[string]time.Time, bool) {
	intents, err := readRebootIntents(path, fs, now)
	if err == nil {
		return intents, true
	}
	if !storeMayHoldLiveIntent(path, now) {
		return a.resetCorruptRebootIntents(path, fs, err)
	}
	slog.Warn("reboot intent store unreadable and recently written; reboot suppressed", "error", err)
	return nil, false
}

// storeMayHoldLiveIntent applies the self-heal rule (C-1a): a bad store
// fails closed only while it could hold an intent younger than the retention
// floor. The age comes from the file's own last-write time; a future or
// unreadable time counts as young.
func storeMayHoldLiveIntent(path string, now time.Time) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	modified := info.ModTime()
	return modified.After(now) || now.Sub(modified) < rebootIntentMinKeep
}

// resetCorruptRebootIntents keeps the bad content as one bounded set-aside copy
// (overwriting any previous one) and starts an empty store (C-1b).
func (a *Agent) resetCorruptRebootIntents(path string, fs stateFS, cause error) (map[string]time.Time, bool) {
	if data := readBoundedForAside(path); len(data) > 0 {
		aside := newAtomicJSONFile(path+rebootIntentAsideSuffix, fs)
		if err := aside.SaveBytes(data); err != nil {
			slog.Warn("cannot keep a copy of the corrupt reboot intent store; reboot suppressed", "error", err)
			return nil, false
		}
	}
	slog.Warn("reboot intent store was corrupt and older than the retention floor; starting a new store",
		"error", cause, "set_aside", path+rebootIntentAsideSuffix)
	return map[string]time.Time{}, true
}

func readBoundedForAside(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, rebootIntentAsideMaxBytes))
	return data
}

// readRebootIntents loads and validates the store. A missing file is an empty
// store. A read error, an oversized file or unparseable content is an error.
func readRebootIntents(path string, fs stateFS, now time.Time) (map[string]time.Time, error) {
	if info, err := os.Stat(path); err == nil && info.Size() > rebootIntentMaxFileBytes {
		return nil, fmt.Errorf("reboot intent store is %d bytes, over the %d byte bound", info.Size(), rebootIntentMaxFileBytes)
	}
	data, err := fs.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]time.Time{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > rebootIntentMaxFileBytes {
		return nil, fmt.Errorf("reboot intent store is %d bytes, over the %d byte bound", len(data), rebootIntentMaxFileBytes)
	}
	return decodeRebootIntents(data, now)
}

// decodeRebootIntents accepts v2 and the legacy v1 format. Legacy entries get
// now as their claim time, which keeps them for a further full retention
// period. Entries with a non-canonical key or an invalid value are dropped:
// they can never match a real claim.
func decodeRebootIntents(data []byte, now time.Time) (map[string]time.Time, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode reboot intents: %w", err)
	}
	if _, isV2 := root["version"]; isV2 {
		var file rebootIntentFile
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("decode reboot intents v2: %w", err)
		}
		return validV2Intents(file.Intents), nil
	}
	intents := make(map[string]time.Time, len(root))
	for id, raw := range root {
		if string(raw) == "true" && models.IsCanonicalUUID(id) {
			intents[id] = now
		}
	}
	return intents, nil
}

func validV2Intents(raw map[string]string) map[string]time.Time {
	intents := make(map[string]time.Time, len(raw))
	for id, text := range raw {
		claimedAt, err := time.Parse(time.RFC3339, text)
		if err != nil || !models.IsCanonicalUUID(id) {
			continue
		}
		intents[id] = claimedAt.UTC()
	}
	return intents
}

func encodeRebootIntents(intents map[string]time.Time) rebootIntentFile {
	file := rebootIntentFile{Version: rebootIntentVersion, Intents: make(map[string]string, len(intents))}
	for id, claimedAt := range intents {
		file.Intents[id] = claimedAt.UTC().Format(time.RFC3339)
	}
	return file
}

// clockMovedBackwards reports whether any claim time is later than now by more
// than the skew allowance. Pruning and eviction are skipped then, because a
// backwards clock would make live intents look old.
func clockMovedBackwards(intents map[string]time.Time, now time.Time) bool {
	for _, claimedAt := range intents {
		if claimedAt.After(now.Add(rebootIntentSkew)) {
			return true
		}
	}
	return false
}

// pruneRebootIntents drops intents older than rebootIntentMaxAge. It is pure.
func pruneRebootIntents(intents map[string]time.Time, now time.Time) (map[string]time.Time, int) {
	if clockMovedBackwards(intents, now) {
		return intents, 0
	}
	cutoff := now.Add(-rebootIntentMaxAge)
	kept := make(map[string]time.Time, len(intents))
	for id, claimedAt := range intents {
		if !claimedAt.Before(cutoff) {
			kept[id] = claimedAt
		}
	}
	return kept, len(intents) - len(kept)
}

// evictForCap makes room for one more intent. Below the cap it changes nothing.
// At the cap it evicts oldest-first (ties by ID), but only entries older than
// rebootIntentMinKeep; if that cannot make room, ok is false. It is pure.
func evictForCap(intents map[string]time.Time, now time.Time) (map[string]time.Time, bool) {
	if len(intents) < rebootIntentCap {
		return copyIntents(intents), true
	}
	if clockMovedBackwards(intents, now) {
		return intents, false
	}
	floor := now.Add(-rebootIntentMinKeep)
	var evictable []string
	for id, claimedAt := range intents {
		if claimedAt.Before(floor) {
			evictable = append(evictable, id)
		}
	}
	needed := len(intents) - rebootIntentCap + 1
	if len(evictable) < needed {
		return intents, false
	}
	sort.Slice(evictable, func(i, j int) bool {
		ti, tj := intents[evictable[i]], intents[evictable[j]]
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return evictable[i] < evictable[j]
	})
	out := copyIntents(intents)
	for _, id := range evictable[:needed] {
		delete(out, id)
	}
	return out, true
}

func copyIntents(intents map[string]time.Time) map[string]time.Time {
	out := make(map[string]time.Time, len(intents)+1)
	for id, claimedAt := range intents {
		out[id] = claimedAt
	}
	return out
}

// reportSuppressedReboot raises one agent warning event when a post-patch
// reboot was suppressed because the intent store could not record it (C-2).
// Both the authorized and the automated path call it, once per command.
func (a *Agent) reportSuppressedReboot(commandID string, claim rebootClaim) {
	var reason string
	switch claim {
	case claimPersistFailed:
		reason = "the reboot intent could not be recorded"
	case claimStoreFull:
		reason = "the reboot intent store is full"
	default:
		return
	}
	msg := fmt.Sprintf("post-patch reboot for command %s was not started: %s", commandID, reason)
	a.notifyEvent(func(ctx context.Context) {
		a.sendAgentEvent(ctx, models.EventCommandCompleted, models.SeverityWarning, msg)
	})
}

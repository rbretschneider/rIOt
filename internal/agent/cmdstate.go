package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// ---------------------------------------------------------------------------
// Clock
// ---------------------------------------------------------------------------

// clock is the agent's injectable time source (NFR-011). Timer-based waits go
// through NewTimer so tests can fire them deterministically. realClock is the
// composition-root default; testable logic never calls time.Now directly.
type clock interface {
	Now() time.Time
	NewTimer(d time.Duration) clockTimer
}

// clockTimer is the subset of *time.Timer the agent uses.
type clockTimer interface {
	C() <-chan time.Time
	Stop() bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTimer(d time.Duration) clockTimer { return realTimer{time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }
func (r realTimer) Stop() bool          { return r.t.Stop() }

// ---------------------------------------------------------------------------
// Durable JSON files
// ---------------------------------------------------------------------------

// stateFS is the file-system seam of the agent's persisted command state. The
// production implementation is osStateFS; tests inject faults (disk full,
// failed rename) and record the operation order (SEC C-4, C-8).
type stateFS interface {
	MkdirAll(path string, perm os.FileMode) error
	// CreateTemp exclusively creates a new file in dir with a random name and
	// mode 0600 (os.CreateTemp semantics).
	CreateTemp(dir, pattern string) (stateFile, error)
	Rename(oldpath, newpath string) error
	ReadFile(path string) ([]byte, error)
	Remove(path string) error
	// SyncDir flushes the directory entry itself so a rename survives power loss.
	SyncDir(dir string) error
}

// stateFile is a file being written by atomicJSONFile.Save.
type stateFile interface {
	Write(p []byte) (int, error)
	Sync() error
	Close() error
	Name() string
}

type osStateFS struct{}

func (osStateFS) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }

func (osStateFS) CreateTemp(dir, pattern string) (stateFile, error) {
	return os.CreateTemp(dir, pattern)
}

func (osStateFS) Rename(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }
func (osStateFS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
func (osStateFS) Remove(path string) error             { return os.Remove(path) }

func (osStateFS) SyncDir(dir string) error {
	if runtime.GOOS == "windows" {
		// Windows cannot open a directory for Sync; the rename is already durable
		// enough for NTFS journaling and there is no portable alternative.
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// atomicJSONFile persists one JSON document with crash-safe replace semantics:
// exclusive-create temp file (0600) in the same directory, fsync, rename over
// the target, fsync the directory (SEC C-8, NFR-009).
type atomicJSONFile struct {
	path string
	fs   stateFS
}

func newAtomicJSONFile(path string, fs stateFS) *atomicJSONFile {
	return &atomicJSONFile{path: path, fs: fs}
}

// Load reads and decodes the document. found is false (with a nil error) when
// the file does not exist yet.
func (f *atomicJSONFile) Load(v any) (found bool, err error) {
	data, err := f.fs.ReadFile(f.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", f.path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return true, fmt.Errorf("decode %s: %w", f.path, err)
	}
	return true, nil
}

// Save marshals v and replaces the file atomically.
func (f *atomicJSONFile) Save(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s: %w", f.path, err)
	}
	return f.SaveBytes(data)
}

// SaveBytes replaces the file with data atomically.
func (f *atomicJSONFile) SaveBytes(data []byte) error {
	dir := filepath.Dir(f.path)
	if err := f.fs.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := f.fs.CreateTemp(dir, filepath.Base(f.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", f.path, err)
	}
	tmpName := tmp.Name()

	if err := f.writeAndSync(tmp, data); err != nil {
		f.discardTemp(tmpName)
		return err
	}
	if err := f.fs.Rename(tmpName, f.path); err != nil {
		f.discardTemp(tmpName)
		return fmt.Errorf("replace %s: %w", f.path, err)
	}
	if err := f.fs.SyncDir(dir); err != nil {
		return fmt.Errorf("sync directory %s: %w", dir, err)
	}
	return nil
}

func (f *atomicJSONFile) writeAndSync(tmp stateFile, data []byte) error {
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	return nil
}

func (f *atomicJSONFile) discardTemp(name string) {
	if err := f.fs.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("cmdstate: could not remove temp file", "path", name, "error", err)
	}
}

// ---------------------------------------------------------------------------
// Seen-command store (FR-057, SEC C-8)
// ---------------------------------------------------------------------------

type seenFile struct {
	Version int                  `json:"version"`
	Seen    map[string]time.Time `json:"seen"`
}

// seenStore remembers command IDs the agent has already accepted so the same
// command is never run twice (WS plus heartbeat delivery, FR-085 re-queue).
// An ID is durable (file and directory fsync) before CheckAndAdd returns nil.
type seenStore struct {
	mu   sync.Mutex
	file *atomicJSONFile
	clk  clock
	seen map[string]time.Time
}

func newSeenStore(path string, fs stateFS, clk clock) *seenStore {
	return &seenStore{file: newAtomicJSONFile(path, fs), clk: clk, seen: map[string]time.Time{}}
}

// Load reads the persisted IDs and drops entries older than SeenMaxAge. A
// missing file is a normal first run; a corrupt file starts empty and is
// reported, because losing the store only re-opens the FR-085 double-delivery
// window and must not stop the agent.
func (s *seenStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var doc seenFile
	found, err := s.file.Load(&doc)
	if err != nil {
		s.seen = map[string]time.Time{}
		if found {
			slog.Warn("cmdstate: seen-command file is corrupt, starting empty", "error", err)
			return nil
		}
		return err
	}
	s.seen = map[string]time.Time{}
	now := s.clk.Now()
	for id, at := range doc.Seen {
		if now.Sub(at) <= models.SeenMaxAge {
			s.seen[id] = at
		}
	}
	return nil
}

// CheckAndAdd records id and reports whether it had been seen before. When the
// new ID cannot be persisted it is still remembered in memory (so this process
// never runs it twice) and the error is returned; the caller decides whether
// the action may run without a durable record (SEC C-8).
func (s *seenStore) CheckAndAdd(id string) (duplicate bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clk.Now()
	s.pruneLocked(now)
	if _, ok := s.seen[id]; ok {
		return true, nil
	}
	s.seen[id] = now
	for len(s.seen) > models.SeenMaxEntries {
		s.dropOldestLocked()
	}
	if err := s.file.Save(seenFile{Version: 1, Seen: s.seen}); err != nil {
		return false, err
	}
	return false, nil
}

// Contains reports whether id is currently remembered.
func (s *seenStore) Contains(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.seen[id]
	return ok
}

func (s *seenStore) pruneLocked(now time.Time) {
	for id, at := range s.seen {
		if now.Sub(at) > models.SeenMaxAge {
			delete(s.seen, id)
		}
	}
}

func (s *seenStore) dropOldestLocked() {
	var oldestID string
	var oldest time.Time
	for id, at := range s.seen {
		if oldestID == "" || at.Before(oldest) || (at.Equal(oldest) && id < oldestID) {
			oldestID, oldest = id, at
		}
	}
	delete(s.seen, oldestID)
}

// ---------------------------------------------------------------------------
// In-flight markers (FR-019, SEC C-4)
// ---------------------------------------------------------------------------

// dockerRecord is the crash-recovery state of one Docker target of a command
// (SEC C-4a). It is written durably before the mutation it describes, so a
// restart in the middle of a recreate can put the set-aside container back.
type dockerRecord struct {
	// Key identifies the record within the marker: the original container name
	// for a standalone update, "compose:<working dir>" for a compose update.
	Key                 string   `json:"key"`
	OriginalName        string   `json:"original_name,omitempty"`
	OldID               string   `json:"old_id,omitempty"`
	SetAsideName        string   `json:"set_aside_name,omitempty"`
	WasRunning          bool     `json:"was_running,omitempty"`
	ReplacementID       string   `json:"replacement_id,omitempty"`
	StoppedDependentIDs []string `json:"stopped_dependent_ids,omitempty"`
	SanitizedFile       string   `json:"sanitized_file,omitempty"`
}

// inflightMarker records a command that has started executing.
type inflightMarker struct {
	Action            string         `json:"action"`
	Target            string         `json:"target,omitempty"`
	Step              string         `json:"step,omitempty"`
	StartedAt         time.Time      `json:"started_at"`
	Reported          bool           `json:"reported,omitempty"`
	UpdateSHA256      string         `json:"update_sha256,omitempty"`
	UpdateRequestedAt time.Time      `json:"update_requested_at,omitempty"`
	Docker            []dockerRecord `json:"docker,omitempty"`
	// RebootIfRequired marks an os_update the operator authorized to reboot
	// after the run, so a result recovered after an agent restart still states
	// the authorization (PATCH-FLOW-001 AD-005).
	RebootIfRequired bool `json:"reboot_if_required,omitempty"`
}

func (m inflightMarker) clone() inflightMarker {
	out := m
	out.Docker = make([]dockerRecord, len(m.Docker))
	for i, r := range m.Docker {
		r.StoppedDependentIDs = append([]string(nil), r.StoppedDependentIDs...)
		out.Docker[i] = r
	}
	return out
}

type inflightFile struct {
	Version  int                        `json:"version"`
	Inflight map[string]*inflightMarker `json:"inflight"`
}

// inflightStore persists the markers. Every mutating method updates memory
// first and then writes the file, returning the write error: a caller that
// must not continue without a durable marker (Docker mutations) checks it.
type inflightStore struct {
	mu   sync.Mutex
	file *atomicJSONFile
	m    map[string]*inflightMarker
}

func (s *inflightStore) SetUpdateHandoff(id, digest string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.m[id]
	if !ok {
		return fmt.Errorf("agent update has no durable command marker")
	}
	if len(digest) != 64 || at.IsZero() {
		return fmt.Errorf("invalid signed update handoff")
	}
	m.UpdateSHA256 = digest
	m.UpdateRequestedAt = at
	return s.saveLocked()
}

func newInflightStore(path string, fs stateFS) *inflightStore {
	return &inflightStore{file: newAtomicJSONFile(path, fs), m: map[string]*inflightMarker{}}
}

// Load reads the persisted markers. A corrupt file starts empty and is
// reported; recovery then has nothing to reconcile.
func (s *inflightStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var doc inflightFile
	found, err := s.file.Load(&doc)
	s.m = map[string]*inflightMarker{}
	if err != nil {
		if found {
			slog.Warn("cmdstate: in-flight marker file is corrupt, starting empty", "error", err)
			return nil
		}
		return err
	}
	for id, m := range doc.Inflight {
		if m != nil {
			s.m[id] = m
		}
	}
	return nil
}

func (s *inflightStore) saveLocked() error {
	return s.file.Save(inflightFile{Version: 1, Inflight: s.m})
}

// Put stores a new marker, replacing any marker with the same ID.
func (s *inflightStore) Put(id string, m inflightMarker) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := m.clone()
	s.m[id] = &c
	return s.saveLocked()
}

// Get returns a copy of the marker.
func (s *inflightStore) Get(id string) (inflightMarker, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.m[id]
	if !ok {
		return inflightMarker{}, false
	}
	return m.clone(), true
}

// SetStep records the last step the command reached.
func (s *inflightStore) SetStep(id, step string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.m[id]
	if !ok {
		return nil
	}
	m.Step = step
	return s.saveLocked()
}

// UpdateDocker applies fn to the Docker record with the given key, creating
// the record when it does not exist, and persists the marker.
func (s *inflightStore) UpdateDocker(id, key string, fn func(*dockerRecord)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.m[id]
	if !ok {
		return fmt.Errorf("no in-flight marker for command %s", id)
	}
	idx := -1
	for i := range m.Docker {
		if m.Docker[i].Key == key {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.Docker = append(m.Docker, dockerRecord{Key: key})
		idx = len(m.Docker) - 1
	}
	fn(&m.Docker[idx])
	return s.saveLocked()
}

// MarkReported records that a failure result has already been queued for this
// marker, so a deferred reconciliation does not report twice.
func (s *inflightStore) MarkReported(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.m[id]
	if !ok {
		return nil
	}
	m.Reported = true
	return s.saveLocked()
}

// Remove deletes the marker.
func (s *inflightStore) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return nil
	}
	delete(s.m, id)
	return s.saveLocked()
}

// inflightEntry pairs a marker with its command ID.
type inflightEntry struct {
	ID     string
	Marker inflightMarker
}

// All returns copies of every marker, oldest first.
func (s *inflightStore) All() []inflightEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]inflightEntry, 0, len(s.m))
	for id, m := range s.m {
		out = append(out, inflightEntry{ID: id, Marker: m.clone()})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Marker.StartedAt.Equal(out[j].Marker.StartedAt) {
			return out[i].Marker.StartedAt.Before(out[j].Marker.StartedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

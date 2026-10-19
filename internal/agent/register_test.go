package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// idFaultFS is the real file system with injectable faults. A fault applies to
// files created in onlyDir, or to every file when onlyDir is empty.
type idFaultFS struct {
	osStateFS

	mu        sync.Mutex
	createErr error
	writeErr  error
	onlyDir   string
	written   map[string]int // bytes written, by directory
	synced    map[string]int // fsyncs, by directory
}

func newIDFaultFS() *idFaultFS {
	return &idFaultFS{written: map[string]int{}, synced: map[string]int{}}
}

func (f *idFaultFS) setFaults(create, write error, onlyDir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createErr, f.writeErr, f.onlyDir = create, write, onlyDir
}

func (f *idFaultFS) applies(dir string) bool { return f.onlyDir == "" || f.onlyDir == dir }

func (f *idFaultFS) CreateTemp(dir, pattern string) (stateFile, error) {
	f.mu.Lock()
	err := f.createErr
	applies := f.applies(dir)
	f.mu.Unlock()
	if err != nil && applies {
		return nil, err
	}
	file, cerr := f.osStateFS.CreateTemp(dir, pattern)
	if cerr != nil {
		return nil, cerr
	}
	return &idFaultFile{stateFile: file, fs: f, dir: dir}, nil
}

type idFaultFile struct {
	stateFile
	fs  *idFaultFS
	dir string
}

func (f *idFaultFile) Write(p []byte) (int, error) {
	f.fs.mu.Lock()
	err := f.fs.writeErr
	applies := f.fs.applies(f.dir)
	f.fs.mu.Unlock()
	if err != nil && applies {
		return 0, err
	}
	n, werr := f.stateFile.Write(p)
	f.fs.mu.Lock()
	f.fs.written[f.dir] += n
	f.fs.mu.Unlock()
	return n, werr
}

func (f *idFaultFile) Sync() error {
	f.fs.mu.Lock()
	f.fs.synced[f.dir]++
	f.fs.mu.Unlock()
	return f.stateFile.Sync()
}

// registerServer answers the registration route and counts its requests.
type registerServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits int
	// onRegister runs while handling a request, before the answer is written.
	onRegister func()
	resp       models.DeviceRegistrationResponse
}

func newRegisterServer(t *testing.T, resp models.DeviceRegistrationResponse) *registerServer {
	t.Helper()
	rs := &registerServer{resp: resp}
	rs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.hits++
		hook := rs.onRegister
		rs.mu.Unlock()
		if hook != nil {
			hook()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rs.resp)
	}))
	t.Cleanup(rs.srv.Close)
	return rs
}

func (r *registerServer) registrations() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits
}

type identityFixture struct {
	agent   *Agent
	fs      *idFaultFS
	srv     *registerServer
	idDir   string
	cfgDir  string
	idPath  string
	cfgPath string
	delays  []time.Duration
	cancel  context.CancelFunc
	ctx     context.Context
}

// newIdentityFixture builds an agent whose ID file and config live in two
// separate temp directories, so a fault can hit one and not the other.
func newIdentityFixture(t *testing.T) *identityFixture {
	t.Helper()
	root := t.TempDir()
	fx := &identityFixture{
		fs:     newIDFaultFS(),
		idDir:  filepath.Join(root, "id-dir"),
		cfgDir: filepath.Join(root, "cfg-dir"),
	}
	fx.idPath = filepath.Join(fx.idDir, "id")
	fx.cfgPath = filepath.Join(fx.cfgDir, "agent.yaml")
	fx.srv = newRegisterServer(t, models.DeviceRegistrationResponse{DeviceID: "dev-123", ShortID: "dev-123", APIKey: "key-xyz"})
	fx.ctx, fx.cancel = context.WithCancel(context.Background())
	t.Cleanup(fx.cancel)

	cfg := DefaultConfig()
	cfg.Server.URL = fx.srv.srv.URL
	fx.agent = &Agent{
		config:     cfg,
		configPath: fx.cfgPath,
		idPath:     fx.idPath,
		fsys:       fx.fs,
		version:    "test",
		client:     NewHTTPClient(fx.srv.srv.URL, ""),
		// The pause records the requested delay and returns at once.
		pause: func(ctx context.Context, d time.Duration) bool {
			fx.delays = append(fx.delays, d)
			return ctx.Err() == nil
		},
	}
	return fx
}

func (fx *identityFixture) dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestGROWTH_AC053_UnwritableIdentityNoRegistration(t *testing.T) {
	logs := captureLogs(t)
	fx := newIdentityFixture(t)
	fx.fs.setFaults(&os.PathError{Op: "open", Path: fx.idDir, Err: syscall.EACCES}, nil, "")
	// Stop the wait after the first failed pre-flight.
	fx.agent.pause = func(ctx context.Context, d time.Duration) bool {
		fx.delays = append(fx.delays, d)
		fx.cancel()
		return false
	}

	err := fx.agent.register(fx.ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, fx.srv.registrations(), "[AC-053] a read-only identity path must never reach Register")
	assert.Empty(t, fx.agent.config.Agent.DeviceID, "[AC-053] the agent must not run under a new ID")
	_, statErr := os.Stat(fx.idPath)
	assert.True(t, os.IsNotExist(statErr), "[AC-053] no ID file is written")
	assert.Contains(t, logs.String(), "level=ERROR")
	// The text handler escapes Windows separators, so match the path's tail.
	assert.Contains(t, logs.String(), "id-dir", "[AC-053] the failing path must be logged")
	assert.Contains(t, logs.String(), identityProbeName)
}

func TestGROWTH_AC053_ENOSPCOnWriteNoRegistration(t *testing.T) {
	logs := captureLogs(t)
	fx := newIdentityFixture(t)
	// The common full-disk case: files can be created (inodes remain) but no data
	// block can be written. An empty create-and-remove pre-flight would pass.
	fx.fs.setFaults(nil, syscall.ENOSPC, "")
	fx.agent.pause = func(ctx context.Context, d time.Duration) bool {
		fx.cancel()
		return false
	}

	err := fx.agent.register(fx.ctx)

	require.Error(t, err)
	assert.Equal(t, 0, fx.srv.registrations(), "[AC-053/C-17] ENOSPC on write must keep the agent from registering")
	assert.Empty(t, fx.dirEntries(t, fx.idDir), "[AC-053] the failed pre-flight must not leave a temp file in the ID directory")
	assert.Contains(t, logs.String(), "no space left", "[AC-053] the error must be logged")
}

func TestGROWTH_SEC016_PreflightBackoff(t *testing.T) {
	logs := captureLogs(t)
	fx := newIdentityFixture(t)
	fx.fs.setFaults(syscall.ENOSPC, nil, "")
	fx.agent.pause = func(ctx context.Context, d time.Duration) bool {
		fx.delays = append(fx.delays, d)
		if len(fx.delays) >= 12 {
			fx.cancel()
			return false
		}
		return true
	}

	err := fx.agent.register(fx.ctx)

	require.ErrorIs(t, err, context.Canceled)
	want := []time.Duration{
		5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second,
		160 * time.Second, 320 * time.Second, 10 * time.Minute, 10 * time.Minute, 10 * time.Minute,
		10 * time.Minute, 10 * time.Minute,
	}
	assert.Equal(t, want, fx.delays, "[SEC-016] 5 s doubling, capped at 10 min")
	assert.Equal(t, 0, fx.srv.registrations(), "[SEC-016] Register must never be called while the pre-flight fails")
	assert.Equal(t, len(want), strings.Count(logs.String(), "cannot write the device identity"),
		"[SEC-016] one log line per backoff step")
}

func TestGROWTH_SEC016_PreflightWritesAndSyncsAtLeast4KiBThroughTheAtomicPath(t *testing.T) {
	captureLogs(t)
	fx := newIdentityFixture(t)

	require.NoError(t, fx.agent.identityPreflightOnce())

	for name, dir := range map[string]string{"ID": fx.idDir, "config": fx.cfgDir} {
		assert.GreaterOrEqual(t, fx.fs.written[dir], 4096, "[SEC-016] the %s directory must see at least 4 KiB written", name)
		assert.GreaterOrEqual(t, fx.fs.synced[dir], 1, "[SEC-016] the %s directory write must be fsynced", name)
		assert.Empty(t, fx.dirEntries(t, dir), "[SEC-016] the pre-flight must remove its files from the %s directory", name)
	}
}

func TestGROWTH_AC053_RegistersAndPersistsWhenWritable(t *testing.T) {
	captureLogs(t)
	fx := newIdentityFixture(t)

	require.NoError(t, fx.agent.register(fx.ctx))

	assert.Equal(t, 1, fx.srv.registrations())
	data, err := os.ReadFile(fx.idPath)
	require.NoError(t, err)
	assert.Equal(t, "dev-123", string(data), "the ID file must hold exactly the device ID")
	assert.Equal(t, "dev-123", fx.agent.config.Agent.DeviceID)
	loaded, err := LoadConfig(fx.cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "key-xyz", loaded.Server.APIKey, "the API key must be saved to the config")
	assert.Equal(t, "key-xyz", fx.agent.client.apiKey)
}

func TestGROWTH_AC053_IDWriteFailingAfterRegistrationIsFatalAndNamesThePath(t *testing.T) {
	captureLogs(t)
	fx := newIdentityFixture(t)
	// The disk fills between the pre-flight and the write.
	fx.srv.onRegister = func() { fx.fs.setFaults(nil, syscall.ENOSPC, "") }

	err := fx.agent.register(fx.ctx)

	require.ErrorIs(t, err, errIdentityNotPersistable, "[AC-053] a new identity that cannot be saved is fatal")
	var pe *identityPersistError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, fx.idPath, pe.Path, "[AC-053] the error must name the ID path")
	assert.ErrorIs(t, pe.Err, syscall.ENOSPC)
	_, statErr := os.Stat(fx.idPath)
	assert.True(t, os.IsNotExist(statErr), "[AC-053] no partial ID file may remain")
}

func TestGROWTH_AC053_ConfigWriteFailingAfterRegistrationIsFatal(t *testing.T) {
	captureLogs(t)
	fx := newIdentityFixture(t)
	fx.srv.onRegister = func() { fx.fs.setFaults(nil, syscall.ENOSPC, fx.cfgDir) }

	err := fx.agent.register(fx.ctx)

	require.ErrorIs(t, err, errIdentityNotPersistable)
	var pe *identityPersistError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, fx.cfgPath, pe.Path)
	data, readErr := os.ReadFile(fx.idPath)
	require.NoError(t, readErr, "the ID is written first, so a lost key re-registers the same device, not a new one")
	assert.Equal(t, "dev-123", string(data))
}

func TestGROWTH_AC053_PeriodicReRegistrationWithTheSameIDOnlyWarns(t *testing.T) {
	logs := captureLogs(t)
	fx := newIdentityFixture(t)
	fx.agent.config.Agent.DeviceID = "dev-123"
	require.NoError(t, os.MkdirAll(fx.idDir, 0o755))
	require.NoError(t, os.WriteFile(fx.idPath, []byte("dev-123"), 0o644))
	fx.fs.setFaults(syscall.ENOSPC, nil, "") // the disk is full now

	require.NoError(t, fx.agent.register(fx.ctx), "[AC-053] a same-ID re-registration must not fail hard")

	assert.Equal(t, 1, fx.srv.registrations(), "a persisted identity must not wait for the pre-flight")
	assert.Contains(t, logs.String(), "could not save the new API key")
	assert.Equal(t, "key-xyz", fx.agent.config.Server.APIKey, "the key is still used from memory")
}

func TestGROWTH_AC053_PeriodicReRegistrationWithADifferentIDIsFatal(t *testing.T) {
	captureLogs(t)
	fx := newIdentityFixture(t)
	fx.agent.config.Agent.DeviceID = "dev-OLD"
	fx.fs.setFaults(syscall.ENOSPC, nil, "")

	err := fx.agent.register(fx.ctx)

	require.ErrorIs(t, err, errIdentityNotPersistable, "[AC-053] an ID mismatch that cannot be saved must fail hard")
}

func TestGROWTH_AC053_ExitPathLogsPathWaitsSixtySecondsAndReturnsTheError(t *testing.T) {
	logs := captureLogs(t)
	fx := newIdentityFixture(t)
	cause := &identityPersistError{Path: fx.idPath, Err: syscall.ENOSPC}

	got := fx.agent.exitUnpersistableIdentity(context.Background(), cause)

	assert.True(t, errors.Is(got, errIdentityNotPersistable), "[AC-053] the error is returned so the process exits non-zero")
	assert.Equal(t, []time.Duration{60 * time.Second}, fx.delays, "[AC-053] the agent waits 60 s before exiting")
	assert.Contains(t, logs.String(), "id-dir", "[AC-053] the path is logged")
	assert.Contains(t, logs.String(), "level=ERROR")
}

package agent

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// AUTOUPD-HARDEN-001: the agent decides where `docker compose` may run. The
// only way to obtain a trustedDir is composeTrust.Check, and composeArgs accepts
// nothing else, so no compose invocation and no sanitized-copy write can skip
// the check (AD-001).

// composeProjectRe is the compose project-name charset. It rules out a leading
// "-", "=" and whitespace, so a project name is always one safe argv token.
var composeProjectRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// Refusal rules (ADD section 6, a closed set).
const (
	ruleNotAbsolute        = "not_absolute"
	ruleWindowsRoots       = "windows_requires_roots"
	ruleResolve            = "resolve"
	ruleStat               = "stat"
	ruleNotDirectory       = "not_directory"
	ruleRootsInvalid       = "compose_roots_invalid"
	ruleOutsideRoots       = "outside_compose_roots"
	ruleInvalidProject     = "invalid_project"
	ruleProjectNotFound    = "project_not_found"
	ruleAmbiguousProject   = "ambiguous_project"
	ruleProjectMismatch    = "project_mismatch"
	ruleWorkDirMismatch    = "work_dir_mismatch"
	ruleLabelProvenance    = "label_provenance"
	ruleDaemonUnavailable  = "daemon_unavailable"
	ruleDockerDataRoot     = "docker_data_root"
	ruleContainerWritable  = "container_writable"
	ruleOwner              = "owner"
	ruleGroupWritable      = "group_writable"
	ruleACL                = "acl"
	ruleWorldWritable      = "world_writable"
	ruleSymlinkEscape      = "symlink_escape"
	ruleNotRegular         = "not_regular"
	ruleNoComposeFile      = "no_compose_file"
	ruleEnvUnreadable      = "env_unreadable"
	ruleEnvUnparsed        = "env_unparsed"
	ruleDirChanged         = "dir_changed"
	defaultDockerSocket    = "/var/run/docker.sock"
	maxTrustFileBytes      = 1 << 20
	accountPasswdPath      = "/etc/passwd"
	accountGroupPath       = "/etc/group"
	accountNSSwitchPath    = "/etc/nsswitch.conf"
	systemdHomedSocketPath = "/run/systemd/userdb/io.systemd.Home"
)

// Compose's own discovery order, copied from the installed compose (v2.26.1 and
// v2.40.3 behave the same): the first existing default name is the base file,
// the first existing override name is merged over it. The orders differ from
// the file names' alphabetical order, so they must not be "tidied".
var (
	composeDefaultNames  = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}
	composeOverrideNames = []string{"compose.override.yml", "compose.override.yaml", "docker-compose.override.yml", "docker-compose.override.yaml"}
)

// systemdUserdbDirs are the standard directories of systemd userdb drop-in
// records (user, group and membership files).
var systemdUserdbDirs = []string{"/etc/userdb", "/run/userdb", "/run/host/userdb", "/usr/lib/userdb"}

// errFileTooLarge is returned by trustFS.ReadFile for a file over the cap.
var errFileTooLarge = errors.New("file exceeds the size cap")

// trustedDir is a compose project directory that passed Check, with the exact
// compose files to load. The zero value is invalid.
type trustedDir struct {
	dir   string
	files []string
}

// Path is the resolved project directory.
func (t trustedDir) Path() string { return t.dir }

// Files is the validated compose file set, as absolute resolved paths.
func (t trustedDir) Files() []string { return append([]string(nil), t.files...) }

func (t trustedDir) valid() bool { return t.dir != "" && len(t.files) > 0 }

// same reports whether two checks resolved the same directory and file set.
func (t trustedDir) same(o trustedDir) bool {
	if t.dir != o.dir || len(t.files) != len(o.files) {
		return false
	}
	for i := range t.files {
		if t.files[i] != o.files[i] {
			return false
		}
	}
	return true
}

// trustRequest names what to check.
type trustRequest struct {
	Dir      string // label or param value, verbatim
	Project  string // project passed to --project-name; "" only in recovery (derive the unique one)
	SourceID string // container whose labels named Dir; "" for stack updates
}

// composeTrustError is a refusal. Its text quotes every label- or
// filesystem-derived value and never carries raw errno text or file content
// (SEC C-5, AC-027).
type composeTrustError struct {
	Rule   string
	Dir    string
	Detail string
}

// Size caps for refusal text: every label- or filesystem-derived value is cut
// to maxRefusalValueBytes before it is quoted, and a whole message to
// maxRefusalBytes, so no input can make a stored result, event or log line grow.
const (
	maxRefusalValueBytes = 200
	maxRefusalBytes      = 1024
)

func (e *composeTrustError) Error() string {
	var msg string
	if e.Dir == "" {
		msg = fmt.Sprintf("compose directory refused (%s): %s", e.Rule, e.Detail)
	} else {
		msg = fmt.Sprintf("compose directory refused (%s): %q: %s", e.Rule, models.TruncateHead(e.Dir, maxRefusalValueBytes), e.Detail)
	}
	return models.TruncateHead(msg, maxRefusalBytes)
}

// newTrustError builds a refusal. String arguments are length-capped before
// formatting; the detail format quotes them with %q.
func newTrustError(rule, dir, format string, args ...interface{}) *composeTrustError {
	capped := make([]interface{}, len(args))
	for i, a := range args {
		if str, ok := a.(string); ok {
			a = models.TruncateHead(str, maxRefusalValueBytes)
		}
		capped[i] = a
	}
	return &composeTrustError{Rule: rule, Dir: dir, Detail: fmt.Sprintf(format, capped...)}
}

// fileMeta is the part of file metadata the validator needs. HasOwner is false
// where the platform has no Unix owner (Windows).
type fileMeta struct {
	Mode     os.FileMode
	UID, GID uint32
	HasOwner bool
}

// trustFS is the filesystem as the validator sees it. Production uses
// osTrustFS; tests use a map-backed fake so no test depends on real ownership.
type trustFS interface {
	Lstat(path string) (fileMeta, error)
	Stat(path string) (fileMeta, error)
	EvalSymlinks(path string) (string, error)
	// ReadFile reads a whole file, or fails with errFileTooLarge over max.
	ReadFile(path string, max int64) ([]byte, error)
	// ReadDirNames lists the entry names of a directory.
	ReadDirNames(path string) ([]string, error)
	HasAccessACL(path string) (bool, error)
	// EUID is the agent's effective user ID, or -1 where there is none.
	EUID() int
}

// osReadFile is the shared osTrustFS.ReadFile.
func osReadFile(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errFileTooLarge
	}
	return data, nil
}

// osReadDirNames is the shared osTrustFS.ReadDirNames.
func osReadDirNames(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Readdirnames(-1)
}

// composeTrust validates compose project directories.
type composeTrust struct {
	fs       trustFS
	goos     string
	socket   string
	roots    []string
	rootsErr error
}

// composeTrust builds the validator from the agent's seams and configuration.
func (a *Agent) composeTrust() *composeTrust {
	var dc DockerConfig
	if a.config != nil {
		dc = a.config.Docker
	}
	t := &composeTrust{fs: a.trustFS, goos: a.trustPlatform, socket: dc.SocketPath, roots: dc.ComposeRoots, rootsErr: dc.rootsErr}
	if t.fs == nil {
		t.fs = osTrustFS{}
	}
	if t.goos == "" {
		t.goos = runtime.GOOS
	}
	return t
}

func (t *composeTrust) windows() bool { return t.goos == "windows" }

func (t *composeTrust) socketPath() string {
	if t.socket != "" {
		return t.socket
	}
	return defaultDockerSocket
}

// within reports whether p is root or below it. The test is separator-aware:
// "/opt/stacks-x" is not inside "/opt/stacks", "/opt/stacks/..foo" is. fold
// compares case-insensitively (Windows).
func within(root, p string, fold bool) bool {
	if fold {
		root, p = strings.ToLower(root), strings.ToLower(p)
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// ---------------------------------------------------------------------------
// Host accounts (AD-003)
// ---------------------------------------------------------------------------

type passwdEntry struct {
	name     string
	uid, gid uint32
}

type groupEntry struct {
	name    string
	gid     uint32
	members []string
}

// parsePasswd and parseGroup return false for anything they cannot be sure
// about: a malformed line, or a "+"/"-" compat line that pulls in NIS entries.
func parsePasswd(data []byte) ([]passwdEntry, bool) {
	var out []passwdEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line[0] == '+' || line[0] == '-' {
			return nil, false
		}
		f := strings.Split(line, ":")
		if len(f) < 4 {
			return nil, false
		}
		uid, err1 := strconv.ParseUint(f[2], 10, 32)
		gid, err2 := strconv.ParseUint(f[3], 10, 32)
		if err1 != nil || err2 != nil || f[0] == "" {
			return nil, false
		}
		out = append(out, passwdEntry{name: f[0], uid: uint32(uid), gid: uint32(gid)})
	}
	return out, true
}

func parseGroup(data []byte) ([]groupEntry, bool) {
	var out []groupEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line[0] == '+' || line[0] == '-' {
			return nil, false
		}
		f := strings.Split(line, ":")
		if len(f) < 4 {
			return nil, false
		}
		gid, err := strconv.ParseUint(f[2], 10, 32)
		if err != nil || f[0] == "" {
			return nil, false
		}
		var members []string
		for _, m := range strings.Split(f[3], ",") {
			if m = strings.TrimSpace(m); m != "" {
				members = append(members, m)
			}
		}
		out = append(out, groupEntry{name: f[0], gid: uint32(gid), members: members})
	}
	return out, true
}

// enumerationEnabled reports whether the account files are the whole story.
// With ldap, sss and the like in nsswitch, users the files do not list can
// exist, so members cannot be enumerated. With systemd, userdb records and
// systemd-homed can add users and group memberships the files do not show.
func (t *composeTrust) enumerationEnabled() bool {
	data, err := t.fs.ReadFile(accountNSSwitchPath, maxTrustFileBytes)
	if err != nil {
		return errors.Is(err, fs.ErrNotExist)
	}
	needSystemd := false
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		db, rest, ok := strings.Cut(line, ":")
		if !ok || (strings.TrimSpace(db) != "passwd" && strings.TrimSpace(db) != "group") {
			continue
		}
		for _, src := range strings.Fields(rest) {
			if strings.HasPrefix(src, "[") && strings.HasSuffix(src, "]") {
				continue // an action such as [NOTFOUND=return]
			}
			switch src {
			case "files", "compat":
			case "systemd":
				needSystemd = true
			default:
				return false
			}
		}
	}
	return !needSystemd || t.userdbClear()
}

// userdbClear reports that systemd has no extra account records: none in the
// standard userdb directories and no systemd-homed service socket.
func (t *composeTrust) userdbClear() bool {
	for _, dir := range systemdUserdbDirs {
		names, err := t.fs.ReadDirNames(dir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return false
		}
		for _, n := range names {
			if strings.Contains(n, ".user") || strings.Contains(n, ".group") || strings.Contains(n, ".membership") {
				return false
			}
		}
	}
	if _, err := t.fs.Stat(systemdHomedSocketPath); err == nil {
		return false
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	return true
}

// trustSet is the accounts whose write access is harmless: those that already
// hold Docker (root-equivalent) rights.
type trustSet struct {
	uids map[uint32]bool
	gids map[uint32]bool
}

// trustSet computes the trusted accounts once per Check (AD-003, no cache).
func (t *composeTrust) trustSet() trustSet {
	ts := trustSet{uids: map[uint32]bool{0: true}, gids: map[uint32]bool{0: true}}
	if e := t.fs.EUID(); e >= 0 {
		ts.uids[uint32(e)] = true
	}
	sock, err := t.fs.Stat(t.socketPath())
	if err != nil {
		return ts
	}
	ts.uids[sock.UID] = true
	effective := sock.Mode.Perm()&0060 == 0060
	if effective {
		ts.gids[sock.GID] = true
	}

	pdata, perr := t.fs.ReadFile(accountPasswdPath, maxTrustFileBytes)
	gdata, gerr := t.fs.ReadFile(accountGroupPath, maxTrustFileBytes)
	if perr != nil || gerr != nil {
		return ts
	}
	passwd, pok := parsePasswd(pdata)
	groups, gok := parseGroup(gdata)
	if !pok || !gok || !t.enumerationEnabled() {
		return ts
	}

	uidOf := map[string]uint32{}
	for _, p := range passwd {
		if _, dup := uidOf[p.name]; !dup {
			uidOf[p.name] = p.uid
		}
	}
	if effective {
		for _, p := range passwd {
			if p.gid == sock.GID {
				ts.uids[p.uid] = true
			}
		}
		for _, g := range groups {
			if g.gid != sock.GID {
				continue
			}
			for _, m := range g.members {
				if uid, ok := uidOf[m]; ok {
					ts.uids[uid] = true
				}
			}
		}
	}

	// User-private groups: a group is trusted when it has members and every one
	// of them is a trusted account.
	for _, g := range groups {
		if ts.gids[g.gid] {
			continue
		}
		var memberUIDs []uint32
		known := true
		for _, m := range g.members {
			uid, ok := uidOf[m]
			if !ok {
				known = false
				break
			}
			memberUIDs = append(memberUIDs, uid)
		}
		if !known {
			continue
		}
		for _, p := range passwd {
			if p.gid == g.gid {
				memberUIDs = append(memberUIDs, p.uid)
			}
		}
		if len(memberUIDs) == 0 {
			continue
		}
		all := true
		for _, u := range memberUIDs {
			if !ts.uids[u] {
				all = false
				break
			}
		}
		if all {
			ts.gids[g.gid] = true
		}
	}
	return ts
}

// ---------------------------------------------------------------------------
// Check
// ---------------------------------------------------------------------------

// checkState is the scratch state of one Check.
type checkState struct {
	t     *composeTrust
	snap  daemonSnapshot
	dir   string // the request's directory, verbatim, for messages
	trust trustSet
	metas map[string]fileMeta
	typeOK,
	leafOK,
	ancOK map[string]bool
	roots     []string
	rootsDone bool
	writables []writableSource
	wrDone    bool
}

func (s *checkState) refuse(rule, format string, args ...interface{}) *composeTrustError {
	return newTrustError(rule, s.dir, format, args...)
}

// Check validates a compose project directory against a fresh daemon snapshot
// and returns the directory and compose files it is safe to run compose with.
// Every refusal is a *composeTrustError. The order of the rules follows the ADD
// section 7.
func (t *composeTrust) Check(snap daemonSnapshot, req trustRequest) (trustedDir, error) {
	td, err := t.check(snap, req)
	if err != nil {
		return trustedDir{}, err
	}
	return td, nil
}

func (t *composeTrust) check(snap daemonSnapshot, req trustRequest) (trustedDir, *composeTrustError) {
	snap = t.bind(snap)
	s := &checkState{
		t: t, snap: snap, dir: req.Dir,
		metas: map[string]fileMeta{}, typeOK: map[string]bool{}, leafOK: map[string]bool{}, ancOK: map[string]bool{},
	}
	win := t.windows()

	if req.Dir == "" || !filepath.IsAbs(req.Dir) {
		return trustedDir{}, s.refuse(ruleNotAbsolute, "the path is not absolute")
	}
	if win && len(t.roots) == 0 {
		return trustedDir{}, s.refuse(ruleWindowsRoots, "compose updates on Windows require docker.compose_roots (ownership cannot be verified)")
	}
	if t.rootsErr != nil {
		return trustedDir{}, s.refuse(ruleRootsInvalid, "docker.compose_roots in agent.yaml has an entry that is not an absolute path; fix it and restart the agent")
	}

	r, err := t.fs.EvalSymlinks(filepath.Clean(req.Dir))
	if err != nil || !filepath.IsAbs(r) {
		return trustedDir{}, s.refuse(ruleResolve, "cannot resolve %q", filepath.Clean(req.Dir))
	}
	if e := s.typeWalk(r); e != nil {
		return trustedDir{}, e
	}
	if e := s.checkRoots(r); e != nil {
		return trustedDir{}, e
	}
	if e := s.checkProject(req, r); e != nil {
		return trustedDir{}, e
	}
	if e := s.checkPlacement(r); e != nil {
		return trustedDir{}, e
	}
	if !win {
		s.trust = t.trustSet()
		if e := s.permWalk(r, true); e != nil {
			return trustedDir{}, e
		}
	}
	files, e := s.checkFiles(r)
	if e != nil {
		return trustedDir{}, e
	}
	return trustedDir{dir: r, files: files}, nil
}

// typeWalk refuses a symlink or a non-directory on the path from p up to the
// root (C-7). The path was resolved, so none should be: a symlink here means
// something changed under the check.
func (s *checkState) typeWalk(path string) *composeTrustError {
	for p := path; ; {
		if s.typeOK[p] {
			return nil
		}
		m, err := s.t.fs.Lstat(p)
		if err != nil {
			return s.refuse(ruleStat, "cannot stat %q", p)
		}
		if m.Mode&os.ModeSymlink != 0 || !m.Mode.IsDir() {
			return s.refuse(ruleNotDirectory, "%q is a symlink or not a directory", p)
		}
		s.metas[p], s.typeOK[p] = m, true
		parent := filepath.Dir(p)
		if parent == p {
			return nil
		}
		p = parent
	}
}

// permWalk applies the ownership and mode rules to path and every ancestor.
// path itself follows the leaf rule, ancestors the ancestor rule. Components
// already checked at the same or a stricter level are not repeated.
func (s *checkState) permWalk(path string, leaf bool) *composeTrustError {
	for p := path; ; leaf = false {
		if s.leafOK[p] || (!leaf && s.ancOK[p]) {
			return nil
		}
		if e := s.dirRule(p, s.metas[p], leaf); e != nil {
			return e
		}
		if leaf {
			s.leafOK[p] = true
		} else {
			s.ancOK[p] = true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil
		}
		p = parent
	}
}

func (s *checkState) ownerTrusted(m fileMeta) bool { return m.HasOwner && s.trust.uids[m.UID] }

// dirRule is the AD-003 table for one directory component.
func (s *checkState) dirRule(p string, m fileMeta, leaf bool) *composeTrustError {
	if !s.ownerTrusted(m) {
		return s.refuse(ruleOwner, "%q is owned by uid %d, which has no Docker rights", p, m.UID)
	}
	perm := m.Mode.Perm()
	sticky := m.Mode&os.ModeSticky != 0
	if perm&0020 != 0 {
		if !s.trust.gids[m.GID] && (leaf || !sticky) {
			return s.refuse(ruleGroupWritable, "%q is mode %04o and writable by gid %d, a group with members that have no Docker rights", p, uint32(perm), m.GID)
		}
		if e := s.aclRule(p); e != nil {
			return e
		}
	}
	if perm&0002 != 0 && (leaf || !sticky) {
		return s.refuse(ruleWorldWritable, "%q is mode %04o and writable by everyone", p, uint32(perm))
	}
	return nil
}

// fileRule is the leaf rule for a file compose loads. Sticky never rescues a
// file or a leaf directory: an untrusted user could still create a new
// override or .env in it.
func (s *checkState) fileRule(p string, m fileMeta) *composeTrustError {
	if !s.ownerTrusted(m) {
		return s.refuse(ruleOwner, "%q is owned by uid %d, which has no Docker rights", p, m.UID)
	}
	perm := m.Mode.Perm()
	if perm&0020 != 0 {
		if !s.trust.gids[m.GID] {
			return s.refuse(ruleGroupWritable, "%q is mode %04o and writable by gid %d, a group with members that have no Docker rights", p, uint32(perm), m.GID)
		}
		if e := s.aclRule(p); e != nil {
			return e
		}
	}
	if perm&0002 != 0 {
		return s.refuse(ruleWorldWritable, "%q is mode %04o and writable by everyone", p, uint32(perm))
	}
	return nil
}

// aclRule refuses a group-writable path that carries an extended access ACL, or
// whose ACL cannot be read (C-1). Only called when the group-write bit is set.
func (s *checkState) aclRule(p string) *composeTrustError {
	has, err := s.t.fs.HasAccessACL(p)
	if err != nil {
		return s.refuse(ruleACL, "%q is group-writable and its access ACL cannot be read", p)
	}
	if has {
		return s.refuse(ruleACL, "%q is group-writable and has an extended access ACL", p)
	}
	return nil
}

// resolvedRoots resolves the configured compose roots once. A root that fails
// to resolve is skipped.
func (s *checkState) resolvedRoots() []string {
	if s.rootsDone {
		return s.roots
	}
	s.rootsDone = true
	for _, r := range s.t.roots {
		if res, err := s.t.fs.EvalSymlinks(filepath.Clean(r)); err == nil {
			s.roots = append(s.roots, res)
		}
	}
	return s.roots
}

// insideRoots reports whether p is inside a configured root, or true when no
// roots are configured.
func (s *checkState) insideRoots(p string) bool {
	if len(s.t.roots) == 0 {
		return true
	}
	for _, r := range s.resolvedRoots() {
		if within(r, p, s.t.windows()) {
			return true
		}
	}
	return false
}

func (s *checkState) checkRoots(r string) *composeTrustError {
	if !s.insideRoots(r) {
		return s.refuse(ruleOutsideRoots, "%q is outside every docker.compose_roots entry", r)
	}
	return nil
}

// checkProject applies label provenance and the one-directory-one-project
// invariant (C-4, SEC-006).
func (s *checkState) checkProject(req trustRequest, r string) *composeTrustError {
	if req.SourceID != "" {
		if _, bad := s.snap.excluded[req.SourceID]; bad {
			return s.refuse(ruleLabelProvenance, "container %q gets its compose working-dir label from its image, so it cannot name a compose directory", s.snap.containerName(req.SourceID))
		}
	}
	if req.Project != "" && !composeProjectRe.MatchString(req.Project) {
		return s.refuse(ruleInvalidProject, "invalid compose project name %q", req.Project)
	}
	projects := s.snap.projectsForDir(req.Dir, r)
	switch {
	case len(projects) == 0:
		return s.refuse(ruleProjectNotFound, "no container of a trusted compose project has this working directory")
	case len(projects) > 1:
		return s.refuse(ruleAmbiguousProject, "more than one compose project claims this working directory: %s", quoteAll(projects))
	case req.Project != "" && projects[0] != req.Project:
		return s.refuse(ruleProjectMismatch, "the directory belongs to compose project %q, not %q", projects[0], req.Project)
	}
	return nil
}

func quoteAll(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = strconv.Quote(models.TruncateHead(s, maxRefusalValueBytes))
	}
	return strings.Join(q, ", ")
}

// ---------------------------------------------------------------------------
// Container-writable locations (AD-014)
// ---------------------------------------------------------------------------

// writableSource is a host location a container can write through a read-write
// mount, volume or bind-device volume.
type writableSource struct {
	container  string
	source     string // as the daemon reports it
	dest       string
	path       string // resolved; unusable when unresolved
	unresolved bool
}

// evalOrClean resolves symlinks, falling back to the cleaned path only when the
// path does not exist.
func (s *checkState) evalMount(p string) (string, bool) {
	res, err := s.t.fs.EvalSymlinks(filepath.Clean(p))
	switch {
	case err == nil:
		return res, true
	case errors.Is(err, fs.ErrNotExist):
		return filepath.Clean(p), true
	}
	return "", false
}

// exemptFromWritable reports that a container mounts the Docker socket, or a
// directory holding it: such a container is already root-equivalent.
func (s *checkState) exemptFromWritable(c container.Summary) bool {
	sock := filepath.Clean(s.t.socketPath())
	sockResolved, _ := s.evalMount(sock)
	for _, m := range c.Mounts {
		if m.Source == "" {
			continue
		}
		src := filepath.Clean(m.Source)
		if within(src, sock, false) {
			return true
		}
		if res, ok := s.evalMount(src); ok && sockResolved != "" && within(res, sockResolved, false) {
			return true
		}
	}
	return false
}

// writableSources lists every read-write source of every container in the
// snapshot, running or stopped, except those of socket-mounting containers.
func (s *checkState) writableSources() []writableSource {
	if s.wrDone {
		return s.writables
	}
	s.wrDone = true
	dataRoots := s.dataRoots()
	for _, c := range s.snap.Containers {
		if s.exemptFromWritable(c) {
			continue
		}
		name := summaryName(c)
		if name == "" {
			name = c.ID
		}
		for _, m := range c.Mounts {
			if !m.RW || (m.Type != mount.TypeBind && m.Type != mount.TypeVolume) {
				continue
			}
			sources := []string{m.Source}
			if dev, ok := s.snap.volumeDevices[m.Name]; ok && m.Type == mount.TypeVolume {
				sources = append(sources, dev)
			}
			for _, src := range sources {
				if src == "" {
					continue
				}
				w := writableSource{container: name, source: src, dest: m.Destination}
				if underAny(dataRoots, filepath.Clean(src), s.t.windows()) {
					continue // covered by docker_data_root
				}
				if res, ok := s.evalMount(src); ok {
					w.path = res
				} else {
					w.unresolved = true
				}
				s.writables = append(s.writables, w)
			}
		}
	}
	return s.writables
}

func underAny(roots []string, p string, fold bool) bool {
	for _, r := range roots {
		if within(r, p, fold) {
			return true
		}
	}
	return false
}

// dataRoots is the Docker data root, lexically and resolved.
func (s *checkState) dataRoots() []string {
	root := filepath.Clean(s.snap.DataRoot)
	roots := []string{root}
	if res, err := s.t.fs.EvalSymlinks(root); err == nil && res != root {
		roots = append(roots, res)
	}
	return roots
}

// checkPlacement refuses a path in the Docker data root or at/under a location
// a container can write (SEC-001b). Only "at or under" is refused: a stack
// directory that is an ancestor of a bind source stays accepted.
func (s *checkState) checkPlacement(p string) *composeTrustError {
	fold := s.t.windows()
	for _, root := range s.dataRoots() {
		if within(root, p, fold) {
			return s.refuse(ruleDockerDataRoot, "%q is under the Docker data root %q", p, root)
		}
	}
	for _, w := range s.writableSources() {
		if w.unresolved {
			return s.refuse(ruleContainerWritable, "cannot resolve %q, the read-write mount (%q → %q) of container %q", w.source, w.source, w.dest, w.container)
		}
		if within(w.path, p, fold) {
			return s.refuse(ruleContainerWritable, "%q is under %q, the read-write mount (%q → %q) of container %q", p, w.path, w.source, w.dest, w.container)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Compose files (AD-005, AD-015)
// ---------------------------------------------------------------------------

type foundFile struct {
	name     string
	resolved string
}

// checkFiles validates every compose, override and .env file present in r and
// returns the exact -f set compose will be given.
func (s *checkState) checkFiles(r string) ([]string, *composeTrustError) {
	win := s.t.windows()
	found := map[string]foundFile{}
	all := append(append(append([]string(nil), composeDefaultNames...), composeOverrideNames...), ".env")
	for _, name := range all {
		p := filepath.Join(r, name)
		m, err := s.t.fs.Lstat(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, s.refuse(ruleStat, "cannot stat %q", p)
		}
		resolved, e := s.checkFileInDir(r, p, m)
		if e != nil {
			return nil, e
		}
		found[name] = foundFile{name: name, resolved: resolved}
	}

	var entries []string
	if env, ok := found[".env"]; ok {
		data, err := s.t.fs.ReadFile(env.resolved, maxTrustFileBytes)
		switch {
		case errors.Is(err, errFileTooLarge):
			return nil, s.refuse(ruleEnvUnparsed, "%q is too large to read", env.resolved)
		case err != nil:
			return nil, s.refuse(ruleEnvUnreadable, "the agent user cannot read %q; compose must read it too — grant the agent user read access (e.g. group docker, mode 0640)", env.resolved)
		}
		files, sep, perr := parseEnvComposeFile(data, win)
		if perr != nil {
			return nil, s.refuse(ruleEnvUnparsed, "%q sets COMPOSE_FILE or COMPOSE_PATH_SEPARATOR in a form the agent cannot interpret with certainty; use one plain line such as COMPOSE_FILE=compose.yaml:extra.yaml", env.resolved)
		}
		if files != "" {
			for _, e := range strings.Split(files, sep) {
				if e == "" {
					return nil, s.refuse(ruleEnvUnparsed, "%q has an empty COMPOSE_FILE entry", env.resolved)
				}
				entries = append(entries, e)
			}
		}
	}

	if entries != nil {
		var out []string
		for _, e := range entries {
			resolved, ce := s.checkComposeFileEntry(r, e)
			if ce != nil {
				return nil, ce
			}
			out = append(out, resolved)
		}
		return out, nil
	}

	var out []string
	for _, name := range composeDefaultNames {
		if f, ok := found[name]; ok {
			out = append(out, f.resolved)
			break
		}
	}
	if len(out) == 0 {
		return nil, s.refuse(ruleNoComposeFile, "no compose file (%s) in the directory and no COMPOSE_FILE in its .env", strings.Join(composeDefaultNames, ", "))
	}
	for _, name := range composeOverrideNames {
		if f, ok := found[name]; ok {
			out = append(out, f.resolved)
			break
		}
	}
	return out, nil
}

// checkFileInDir validates one file found directly in r and returns the path to
// load: the file itself, or, for a symlink, its target. A symlink must end in a
// regular file inside r.
func (s *checkState) checkFileInDir(r, p string, m fileMeta) (string, *composeTrustError) {
	win := s.t.windows()
	resolved := p
	if m.Mode&os.ModeSymlink != 0 {
		if !win && !s.ownerTrusted(m) {
			return "", s.refuse(ruleOwner, "%q is owned by uid %d, which has no Docker rights", p, m.UID)
		}
		target, err := s.t.fs.EvalSymlinks(p)
		if err != nil {
			return "", s.refuse(ruleResolve, "cannot resolve %q", p)
		}
		if !within(r, target, win) {
			return "", s.refuse(ruleSymlinkEscape, "%q resolves to %q, outside the directory", p, target)
		}
		tm, err := s.t.fs.Lstat(target)
		if err != nil {
			return "", s.refuse(ruleStat, "cannot stat %q", target)
		}
		if !tm.Mode.IsRegular() {
			return "", s.refuse(ruleNotRegular, "%q is not a regular file", target)
		}
		if dir := filepath.Dir(target); dir != r {
			if e := s.typeWalk(dir); e != nil {
				return "", e
			}
			if !win {
				if e := s.permWalk(dir, true); e != nil {
					return "", e
				}
			}
		}
		m, resolved = tm, target
	} else if !m.Mode.IsRegular() {
		return "", s.refuse(ruleNotRegular, "%q is not a regular file", p)
	}
	if !win {
		if e := s.fileRule(resolved, m); e != nil {
			return "", e
		}
	}
	if e := s.checkPlacement(resolved); e != nil {
		return "", e
	}
	return resolved, nil
}

// checkComposeFileEntry validates one COMPOSE_FILE entry, which may live outside
// the project directory, and returns the resolved path to pass to compose.
func (s *checkState) checkComposeFileEntry(r, entry string) (string, *composeTrustError) {
	win := s.t.windows()
	joined := entry
	if !filepath.IsAbs(entry) {
		joined = filepath.Join(r, entry)
	}
	joined = filepath.Clean(joined)
	resolved, err := s.t.fs.EvalSymlinks(joined)
	if err != nil || !filepath.IsAbs(resolved) {
		return "", s.refuse(ruleResolve, "cannot resolve %q", joined)
	}
	m, err := s.t.fs.Lstat(resolved)
	if err != nil {
		return "", s.refuse(ruleStat, "cannot stat %q", resolved)
	}
	if !m.Mode.IsRegular() {
		return "", s.refuse(ruleNotRegular, "%q is not a regular file", resolved)
	}
	dir := filepath.Dir(resolved)
	if e := s.typeWalk(dir); e != nil {
		return "", e
	}
	if !win {
		if e := s.permWalk(dir, true); e != nil {
			return "", e
		}
		if e := s.fileRule(resolved, m); e != nil {
			return "", e
		}
	}
	if e := s.checkPlacement(resolved); e != nil {
		return "", e
	}
	if !s.insideRoots(resolved) {
		return "", s.refuse(ruleOutsideRoots, "%q is outside every docker.compose_roots entry", resolved)
	}
	return resolved, nil
}

// envLineValue matches the forms of a COMPOSE_FILE / COMPOSE_PATH_SEPARATOR line
// the agent accepts (AD-015). Everything else on a candidate line is refused.
var envLineValue = regexp.MustCompile(
	`^(COMPOSE_FILE|COMPOSE_PATH_SEPARATOR)=(` +
		"[^\\s\"'$\\\\#`]+" + // unquoted
		`|'[^'\n]*'` + // single-quoted
		`|"[^"$\\` + "`" + `]*"` + // double-quoted
		`)$`)

// parseEnvComposeFile extracts COMPOSE_FILE and the list separator from a
// project .env. It errors on anything it cannot read with certainty: a
// candidate line in an unmodelled form, a duplicate key, an empty value.
// Mis-parsing is a functional problem only: compose is later given the
// validated files as explicit -f tokens and loads nothing else. The content is
// never placed in the error.
func parseEnvComposeFile(data []byte, windows bool) (files, sep string, err error) {
	sep = ":"
	if windows {
		sep = ";"
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // a UTF-8 byte order mark, which compose also skips
	seen := map[string]bool{}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		lt := strings.TrimLeft(line, " \t")
		if !isEnvCandidate(lt) {
			continue
		}
		lt = strings.TrimRight(lt, " \t")
		m := envLineValue.FindStringSubmatch(lt)
		if m == nil {
			return "", "", errors.New("unsupported form")
		}
		key, val := m[1], m[2]
		if seen[key] {
			return "", "", errors.New("duplicate key")
		}
		seen[key] = true
		if len(val) >= 2 && (val[0] == '\'' || val[0] == '"') {
			val = val[1 : len(val)-1]
		}
		if val == "" {
			return "", "", errors.New("empty value")
		}
		if key == "COMPOSE_FILE" {
			files = val
		} else {
			sep = val
		}
	}
	return files, sep, nil
}

// isEnvCandidate reports whether a left-trimmed .env line mentions one of the
// two variables the agent models.
func isEnvCandidate(lt string) bool {
	for _, key := range []string{"COMPOSE_FILE", "COMPOSE_PATH_SEPARATOR"} {
		if hasEnvKey(lt, key) {
			return true
		}
		if rest, ok := strings.CutPrefix(lt, "export"); ok && len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t') {
			if hasEnvKey(strings.TrimLeft(rest, " \t"), key) {
				return true
			}
		}
	}
	return false
}

// hasEnvKey reports whether s starts with exactly the variable name key: the
// next character must end the name (end of line, whitespace or "="). A name that
// merely shares the prefix, such as COMPOSE_FILES_DIR, is another variable.
func hasEnvKey(s, key string) bool {
	rest, ok := strings.CutPrefix(s, key)
	if !ok {
		return false
	}
	return rest == "" || rest[0] == '=' || rest[0] == ' ' || rest[0] == '\t'
}

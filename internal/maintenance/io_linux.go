//go:build linux

package maintenance

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type Lock struct{ f *os.File }

func AcquireLock() (*Lock, error) { return AcquireLockAt(LockPath) }
func AcquireLockAt(path string) (*Lock, error) {
	dir, base, err := openParent(path, -1)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), base, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if err = validateFile(f, 0, 0600); err != nil {
		f.Close()
		return nil, err
	}
	var lockStat unix.Stat_t
	if err = unix.Fstat(fd, &lockStat); err != nil || lockStat.Gid != 0 || lockStat.Mode&0777 != 0600 {
		f.Close()
		return nil, errors.New("maintenance lock must be root:root 0600")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return &Lock{f: f}, nil
}
func (l *Lock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// openParent pins each directory inode while walking without following links.
// Only the fixed agent-owned leaf directory may differ from root ownership.
func openParent(path string, agentUID int) (*os.File, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, "", errors.New("noncanonical absolute path")
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) == 0 {
		return nil, "", errors.New("empty path")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", err
	}
	dir := os.NewFile(uintptr(fd), "/")
	for i, name := range parts[:len(parts)-1] {
		next, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		dir.Close()
		if err != nil {
			return nil, "", err
		}
		dir = os.NewFile(uintptr(next), name)
		var st unix.Stat_t
		if err = unix.Fstat(next, &st); err != nil {
			dir.Close()
			return nil, "", err
		}
		uidOK := st.Uid == 0 || (i == len(parts)-2 && agentUID >= 0 && st.Uid == uint32(agentUID))
		// Root-owned sticky ancestors (/tmp in fixtures and /run/lock on
		// Debian) cannot replace a protected root-owned descendant inode.
		sticky := st.Uid == 0 && st.Mode&unix.S_ISVTX != 0
		if !uidOK || (st.Mode&0022 != 0 && !sticky) {
			dir.Close()
			return nil, "", errors.New("unsafe path parent ownership or permissions")
		}
	}
	return dir, parts[len(parts)-1], nil
}
func validateFile(f *os.File, uid int, maxMode uint32) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(uid) || st.Mode&0777&^maxMode != 0 || st.Nlink != 1 {
		return errors.New("unsafe regular file ownership, mode or links")
	}
	return nil
}
func readSafe(path string, max int64, uid, agentUID int) ([]byte, error) {
	dir, base, err := openParent(path, agentUID)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), base, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	if err = validateFile(f, uid, 0640); err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > max {
		return nil, errors.New("file exceeds maintenance size limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errors.New("file grew beyond maintenance size limit")
	}
	return data, nil
}
func agentIdentity() (int, int, error) {
	u, err := user.Lookup("riot")
	if err != nil {
		return 0, 0, err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, err
	}
	gid, err := strconv.Atoi(u.Gid)
	return uid, gid, err
}
func ReadAgentFile(path string, max int64) ([]byte, error) {
	uid, _, err := agentIdentity()
	if err != nil {
		return nil, err
	}
	return readSafe(path, max, uid, uid)
}
func ReadInbox() (Request, error) {
	data, err := ReadAgentFile(InboxPath, MaxRequestBytes)
	if err != nil {
		return Request{}, err
	}
	return ParseRequest(data)
}

func WriteRequest(req Request) error {
	if err := ValidateRequest(req); err != nil {
		return err
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	dir, base, err := openParent(InboxPath, os.Geteuid())
	if err != nil {
		return err
	}
	defer dir.Close()
	return atomicWrite(dir, base, data, 0600, os.Geteuid(), os.Getegid(), false)
}
func ReadRecord(path string) (Record, error) {
	data, err := readSafe(path, MaxRecordBytes, 0, -1)
	if err != nil {
		return Record{}, err
	}
	return parseRecord(data)
}
func ReadCurrent() (Record, error) { return ReadRecord(filepath.Join(StateDir, "current.json")) }
func ReadTerminal(id string) (Record, error) {
	return readTerminalAt(StateDir, id)
}
func readTerminalAt(dir, id string) (Record, error) {
	if !ValidCommandID(id) {
		return Record{}, errors.New("invalid command UUID")
	}
	rec, err := ReadRecord(filepath.Join(dir, id+".json"))
	if err != nil {
		return Record{}, err
	}
	if rec.CommandID != id || !rec.Terminal() {
		return Record{}, errors.New("terminal maintenance record does not match requested command and outcome")
	}
	return rec, nil
}
func ReadUpdateStatus() (UpdateStatus, error) {
	var status UpdateStatus
	data, err := readSafe(filepath.Join(StateDir, "update.json"), 8192, 0, -1)
	if err != nil {
		return status, err
	}
	if err = rejectDuplicateKeys(data); err != nil {
		return status, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&status); err != nil {
		return status, err
	}
	if d.Decode(new(any)) != io.EOF {
		return status, errors.New("trailing update status JSON")
	}
	return status, status.Validate()
}
func WriteUpdateStatus(status UpdateStatus) error {
	if err := status.Validate(); err != nil {
		return err
	}
	store, err := OpenStore()
	if err != nil {
		return err
	}
	defer store.Close()
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	if len(data) > 8192 {
		return errors.New("update handoff status exceeds encoded bound")
	}
	return atomicWrite(store.dir, "update.json", data, 0640, 0, store.gid, false)
}

func atomicWrite(dir *os.File, base string, data []byte, mode uint32, uid, gid int, exclusive bool) error {
	if filepath.Base(base) != base {
		return errors.New("invalid record basename")
	}
	// Refuse an existing unsafe destination even though rename would replace
	// the link itself: a forged state must never be silently repaired.
	fd, err := unix.Openat(int(dir.Fd()), base, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err == nil {
		f := os.NewFile(uintptr(fd), base)
		err = validateFile(f, uid, mode)
		f.Close()
		if exclusive && err == nil {
			err = os.ErrExist
		}
		if err != nil {
			return err
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := ".record-" + hex.EncodeToString(nonce[:])
	fd, err = unix.Openat(int(dir.Fd()), tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, mode)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), tmp)
	// Failed temporary files are retained for operator review; no record or
	// arbitrary filesystem path is deleted by the privileged service.
	if err = unix.Fchown(fd, uid, gid); err != nil {
		f.Close()
		return err
	}
	if err = unix.Fchmod(fd, mode); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if exclusive {
		err = unix.Renameat2(int(dir.Fd()), tmp, int(dir.Fd()), base, unix.RENAME_NOREPLACE)
	} else {
		err = unix.Renameat(int(dir.Fd()), tmp, int(dir.Fd()), base)
	}
	if err != nil {
		return err
	}
	return dir.Sync()
}

type Store struct {
	dir  *os.File
	path string
	gid  int
}

func OpenStore() (*Store, error) {
	_, gid, err := agentIdentity()
	if err != nil {
		return nil, err
	}
	return openStore(StateDir, gid)
}
func openStore(path string, gid int) (*Store, error) {
	parent, base, err := openParent(filepath.Join(path, "current.json"), -1)
	if err != nil {
		return nil, err
	}
	_ = base
	var st unix.Stat_t
	if err = unix.Fstat(int(parent.Fd()), &st); err != nil {
		parent.Close()
		return nil, err
	}
	if st.Uid != 0 || st.Gid != uint32(gid) || st.Mode&0777 != 0750 {
		parent.Close()
		return nil, errors.New("root state must be root:riot 0750")
	}
	return &Store{dir: parent, path: path, gid: gid}, nil
}
func (s *Store) Close() error             { return s.dir.Close() }
func (s *Store) Current() (Record, error) { return ReadRecord(filepath.Join(s.path, "current.json")) }
func (s *Store) Begin(rec Record) error {
	if err := rec.Validate(); err != nil {
		return err
	}
	if old, err := s.Current(); err == nil {
		if old.CommandID == rec.CommandID {
			return errors.New("maintenance command already recorded")
		}
		if !old.Terminal() || old.HoldsReleased {
			return errors.New("unfinished maintenance requires recovery")
		}
		// A failed archive write must never be erased by the next current
		// record, even if the transaction itself reached a terminal phase.
		if archived, archiveErr := ReadRecord(filepath.Join(s.path, old.CommandID+".json")); archiveErr != nil || !archived.Terminal() || archived.CommandID != old.CommandID {
			return errors.New("previous terminal result was not durably archived; root review required")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := ReadRecord(filepath.Join(s.path, rec.CommandID+".json")); err == nil {
		return errors.New("maintenance command already completed")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.capacity(); err != nil {
		return err
	}
	return s.Save(rec)
}
func (s *Store) capacity() error {
	entries, err := s.dir.ReadDir(-1)
	if err != nil {
		return err
	}
	if _, err = s.dir.Seek(0, 0); err != nil {
		return err
	}
	count := 0
	var total int64
	for _, e := range entries {
		if e.Name() == "current.json" {
			continue
		}
		if e.Name() == "update.json" {
			if _, err := readSafe(filepath.Join(s.path, e.Name()), 8192, 0, -1); err != nil {
				return err
			}
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if !ValidCommandID(id) || e.Name() != id+".json" {
			return errors.New("unrecognized root state entry requires operator review")
		}
		data, err := readSafe(filepath.Join(s.path, e.Name()), MaxRecordBytes, 0, -1)
		if err != nil {
			return err
		}
		r, err := parseRecord(data)
		if err != nil || !r.Terminal() || r.CommandID != id {
			return errors.New("invalid retained maintenance record")
		}
		count++
		total += int64(len(data))
	}
	if count >= MaxTerminalRecords || total+MaxRecordBytes > MaxHistoryBytes {
		return ErrHistoryFull
	}
	return nil
}
func (s *Store) Save(rec Record) error {
	if err := rec.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if len(data) > MaxRecordBytes {
		return errors.New("maintenance record too large")
	}
	return atomicWrite(s.dir, "current.json", data, 0640, 0, s.gid, false)
}
func (s *Store) Finish(rec Record) error {
	if !rec.Terminal() {
		return errors.New("cannot finish nonterminal maintenance")
	}
	if err := rec.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if len(data) > MaxRecordBytes {
		return errors.New("maintenance record too large")
	}
	path := filepath.Join(s.path, rec.CommandID+".json")
	if prior, err := ReadRecord(path); err == nil {
		if prior.CommandID == rec.CommandID && prior.Terminal() {
			return s.Save(rec)
		}
		return errors.New("conflicting terminal record")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = atomicWrite(s.dir, rec.CommandID+".json", data, 0640, 0, s.gid, true); err != nil {
		return fmt.Errorf("archive terminal record: %w", err)
	}
	return s.Save(rec)
}

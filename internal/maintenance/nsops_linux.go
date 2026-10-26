//go:build linux

package maintenance

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

// statusFD is the launcher's status pipe: ExtraFiles[0] of the worker's command.
const statusFD = 3

// linuxOps is the real nsOps. It is the only code that changes the mount
// namespace.
type linuxOps struct{}

// RunExtraMain is the entry of `riot-maintenance --run-extra <id>`. It returns
// only on failure: success replaces the process with the updater.
func RunExtraMain(args []string) *LauncherError {
	return RunExtra(linuxOps{}, args)
}

func (linuxOps) Euid() int                { return os.Geteuid() }
func (linuxOps) Getenv(key string) string { return os.Getenv(key) }

func (linuxOps) StatusPipeIsFIFO() error {
	var st unix.Stat_t
	if err := unix.Fstat(statusFD, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFIFO {
		return errors.New("not a FIFO")
	}
	return nil
}

func (linuxOps) ParentExe() (string, error) {
	return os.Readlink(fmt.Sprintf("/proc/%d/exe", os.Getppid()))
}

// LockThread pins this goroutine to its OS thread: the mount namespace belongs
// to the thread, and exec must run on the thread that unshared.
func (linuxOps) LockThread() { runtime.LockOSThread() }

func (linuxOps) Unshare(flags int) error { return unix.Unshare(flags) }

func (linuxOps) Mount(source, target, fstype string, flags uintptr, data string) error {
	return unix.Mount(source, target, fstype, flags, data)
}

func (linuxOps) ReadFile(path string, max int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > max {
		return nil, errors.New("file exceeds its bound")
	}
	return data, nil
}

func (linuxOps) StatfsType(path string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Type), nil
}

func (linuxOps) StatID(path string) (FileID, error) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return FileID{}, err
	}
	return FileID{Dev: uint64(st.Dev), Ino: st.Ino, UID: st.Uid, Mode: st.Mode}, nil
}

func (linuxOps) StatNoFollow(path string) (FileID, error) {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return FileID{}, err
	}
	return FileID{Dev: uint64(st.Dev), Ino: st.Ino, UID: st.Uid, Mode: st.Mode}, nil
}

func (linuxOps) EntryCount(path string) (int, error) {
	d, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	return len(names), err
}

func (linuxOps) Chdir(path string) error { return unix.Chdir(path) }

func (linuxOps) WriteStatus(line string) error {
	_, err := unix.Write(statusFD, []byte(line))
	return err
}

func (linuxOps) SetStatusCloexec() error {
	_, err := unix.FcntlInt(statusFD, unix.F_SETFD, unix.FD_CLOEXEC)
	return err
}

func (linuxOps) Exec(path string, argv, env []string) error { return syscall.Exec(path, argv, env) }

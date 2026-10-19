//go:build linux

package agent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// secureDockerRead opens every path component without following symlinks.
func secureDockerRead(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("absolute path required")
	}
	clean := filepath.Clean(path)
	if clean != path {
		return nil, fmt.Errorf("unclean file path")
	}
	for _, root := range []string{"/proc", "/sys", "/dev", "/root", "/etc/riot", "/var/lib/riot"} {
		if clean == root || strings.HasPrefix(clean, root+"/") {
			return nil, fmt.Errorf("denied path")
		}
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	for i, p := range parts {
		flags := syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK | syscall.O_CLOEXEC
		if i < len(parts)-1 {
			flags |= syscall.O_DIRECTORY
		}
		next, e := syscall.Openat(fd, p, flags, 0)
		syscall.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), clean)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("regular file required")
	}
	if info.Size() > 1<<20 {
		return nil, errFileTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if len(data) > 1<<20 {
		return nil, errFileTooLarge
	}
	return data, err
}

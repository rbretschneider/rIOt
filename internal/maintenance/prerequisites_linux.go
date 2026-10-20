//go:build linux

package maintenance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func CheckTrustedExecutable(path string) error {
	dir, base, err := openParent(path, -1)
	if err != nil {
		return err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), base, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	if err = validateFile(f, 0, 0755); err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Mode()&0111 == 0 {
		return errors.New("trusted executable has no execute bit")
	}
	return nil
}
func CheckPrerequisites(ctx context.Context) error {
	for _, path := range []string{"/usr/bin/dpkg", "/usr/bin/dpkg-query", "/usr/bin/apt-get", "/usr/bin/apt-mark", "/usr/bin/systemctl"} {
		if err := CheckTrustedExecutable(path); err != nil {
			return fmt.Errorf("unsupported package tool %s: %w", path, err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, spec := range []struct {
		path    string
		minimum int
		prefix  string
	}{{"/usr/bin/apt-get", 2, "apt"}, {"/usr/bin/systemctl", 241, "systemd"}} {
		res, err := runProcess(ctx, spec.path, []string{"--version"}, time.Second, io.Discard, io.Discard)
		if err != nil {
			return fmt.Errorf("read prerequisite version: %w", err)
		}
		if !supportedVersion(res.Output, spec.prefix, spec.minimum) {
			return fmt.Errorf("%w: requires %s >= %d", ErrUnsupported, spec.prefix, spec.minimum)
		}
	}
	return nil
}
func supportedVersion(output, prefix string, minimum int) bool {
	fields := strings.Fields(output)
	if len(fields) < 2 || fields[0] != prefix {
		return false
	}
	major := strings.SplitN(fields[1], ".", 2)[0]
	n, err := strconv.Atoi(major)
	return err == nil && n >= minimum
}

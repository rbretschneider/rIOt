//go:build linux

package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/maintenance"
)

// boundedWriter keeps at most limit bytes and records that more arrived.
type boundedWriter struct {
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if room := w.limit - w.buf.Len(); len(p) > room {
		w.exceeded = true
		if room > 0 {
			w.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return w.buf.Write(p)
}

// extraCapabilities runs the installed helper's --capabilities mode as the
// unprivileged agent user. The helper must pass the same root-owned path check
// as the apply helper, the run is cut at 5 seconds, the output at 256 bytes and
// the environment is fixed (SEC-007). The mode needs no root and touches no lock
// or file, and it is not an updater.
func extraCapabilities(ctx context.Context) ([]byte, error) {
	return capabilitiesAt(ctx, maintenance.HelperPath, trustedMaintenanceFile, capabilitiesTimeout)
}

// capabilitiesTimeout bounds the helper probe.
const capabilitiesTimeout = 5 * time.Second

// capabilitiesAt is extraCapabilities over an explicit path, trust check and
// timeout, so a test can drive it with a shell fixture instead of the helper.
func capabilitiesAt(ctx context.Context, helper string, trust func(string, bool) error, timeout time.Duration) ([]byte, error) {
	if err := trust(helper, true); err != nil {
		return nil, err
	}
	probe, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(probe, helper, "--capabilities")
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C"}
	cmd.Dir = "/"
	cmd.WaitDelay = time.Second
	out := &boundedWriter{limit: maxCapabilitiesBytes + 1}
	cmd.Stdout, cmd.Stderr = out, io.Discard
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	if out.exceeded || out.buf.Len() > maxCapabilitiesBytes {
		return nil, errors.New("helper capabilities exceed their bound")
	}
	return out.buf.Bytes(), nil
}

// extraRootPolicy reads the root-owned opt-in policy. Every directory on the path
// and the file itself must be root-owned and not group or world writable, the
// file must be regular and unlinked, and at most 512 bytes are read.
func extraRootPolicy() ([]byte, error) {
	if err := trustedMaintenanceFile(extraupd.PolicyPath, false); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(extraupd.PolicyPath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, extraupd.MaxPolicyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > extraupd.MaxPolicyBytes {
		return nil, errors.New("policy file exceeds its bound")
	}
	return data, nil
}

// extraSignature identifies the helper, the policy and the drop-in by device,
// inode, size, mtime and ctime, so the cached facts are dropped as soon as any
// of them is replaced.
func extraSignature() string {
	var parts []string
	for _, path := range []string{maintenance.HelperPath, extraupd.PolicyPath, extraupd.PolicyDropInPath} {
		st, err := os.Lstat(path)
		if err != nil {
			parts = append(parts, "absent")
			continue
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok {
			return ""
		}
		parts = append(parts, fmt.Sprintf("%d:%d:%d:%d:%d", sys.Dev, sys.Ino, st.Size(), st.ModTime().UnixNano(), sys.Ctim.Nano()))
	}
	return strings.Join(parts, "|")
}

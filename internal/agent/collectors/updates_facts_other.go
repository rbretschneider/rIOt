//go:build !linux

package collectors

import (
	"context"
	"errors"
)

// runReleaseCheck is unsupported off Linux; the caller reports "unavailable".
func runReleaseCheck(context.Context) (int, string, error) {
	return -1, "", errors.New("release check is only supported on linux")
}

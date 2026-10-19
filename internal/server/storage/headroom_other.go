//go:build !linux && !windows

package storage

import "errors"

func diskSpace(path string) (free, total int64, err error) {
	return 0, 0, errors.New("filesystem probe unsupported")
}

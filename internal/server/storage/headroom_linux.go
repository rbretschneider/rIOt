package storage

import "golang.org/x/sys/unix"

func diskSpace(path string) (free, total int64, err error) {
	var s unix.Statfs_t
	err = unix.Statfs(path, &s)
	return int64(s.Bavail) * s.Bsize, int64(s.Blocks) * s.Bsize, err
}

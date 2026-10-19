package storage

import "golang.org/x/sys/windows"

func diskSpace(path string) (free, total int64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var f, t uint64
	err = windows.GetDiskFreeSpaceEx(p, &f, &t, nil)
	return int64(f), int64(t), err
}

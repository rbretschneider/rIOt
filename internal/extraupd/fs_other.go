//go:build !linux

package extraupd

import "os"

// openNoFollow is the portable fallback used for fixture trees on development
// hosts. ReadFile has already refused anything that is not a regular file.
func openNoFollow(path string) (*os.File, error) { return os.Open(path) }

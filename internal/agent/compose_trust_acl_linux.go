//go:build linux

package agent

import "syscall"

// accessACLPresent reports whether path carries an extended POSIX access ACL.
// The kernel stores the xattr only for ACLs that go beyond the mode bits, so its
// presence means an extended ACL. ENODATA and ENOTSUP mean there is none.
func accessACLPresent(path string) (bool, error) {
	_, err := syscall.Getxattr(path, "system.posix_acl_access", nil)
	switch err {
	case nil:
		return true, nil
	case syscall.ENODATA, syscall.ENOTSUP:
		return false, nil
	}
	return false, err
}

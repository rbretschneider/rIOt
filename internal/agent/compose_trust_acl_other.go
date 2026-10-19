//go:build !linux && !windows

package agent

import "errors"

// accessACLPresent cannot be answered off Linux, so a group-writable path there
// fails closed (the validator refuses it with rule acl).
func accessACLPresent(string) (bool, error) {
	return false, errors.New("access ACL lookup is not supported on this platform")
}

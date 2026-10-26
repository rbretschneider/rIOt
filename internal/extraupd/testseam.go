package extraupd

import "os"

// This file is the only place that may define DirFSOwnedBy. A guard test fails
// on any reference to the identifier from non-test code under cmd/ and
// internal/ (DP-SEC-006).

// DirFSOwnedBy returns an FS rooted at a directory that believes flags owned by
// uid. It exists for test fixtures only, where temp trees belong to the test's
// user and not to root. No production code may use it.
func DirFSOwnedBy(root string, uid uint32) FS { return dirFS{root: root, owner: uid} }

// DirFSOwnedByCaller is DirFSOwnedBy for the user running the test. It is the
// shared fixture helper of the packages that build host trees in temp dirs.
func DirFSOwnedByCaller(root string) FS { return DirFSOwnedBy(root, uint32(os.Getuid())) }

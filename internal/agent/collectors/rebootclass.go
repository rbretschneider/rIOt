package collectors

import (
	"regexp"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Reboot-class package classes (PATCH-GATE FR-001, FR-007). A package whose
// class is ClassGPUDriver or ClassKernel is "reboot-class": applying it
// requires a reboot to keep the kernel module and user-space halves in sync.
const (
	ClassGPUDriver = "gpu_driver"
	ClassKernel    = "kernel"
)

// packageNameRe is the strict allowlist for package names that may reach
// privileged package-manager argv, the dnf excludepkgs fragment, or the hold
// state file (PATCH-GATE AD-016, SEC-PATCH-GATE-003). The leading
// alphanumeric anchor blocks option injection ("-o ..." can never pass as a
// name); the charset excludes ',', newlines, '[', ']', '=', '#', and all
// whitespace so no name can break out of the single excludepkgs= ini value.
var packageNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+:~_-]*$`)

// ValidPackageName reports whether a package name is safe to pass to
// privileged package-manager argv and to embed in the dnf fragment.
func ValidPackageName(name string) bool {
	return packageNameRe.MatchString(name)
}

// classRule is one entry in the ordered classification table. First match
// wins, which gives exclusions priority over GPU rules and GPU rules
// priority over kernel rules (FR-006 GPU-over-kernel precedence).
type classRule struct {
	match func(name string) bool
	class string
}

func exact(s string) func(string) bool {
	return func(name string) bool { return name == s }
}

func prefix(p string) func(string) bool {
	return func(name string) bool { return strings.HasPrefix(name, p) }
}

func suffix(s string) func(string) bool {
	return func(name string) bool { return strings.HasSuffix(name, s) }
}

// rebootClassRules is the single shared pattern table used by both the apt
// and dnf paths (FR-007 — one table so the two package managers cannot
// drift). Rule order is load-bearing: exclusions → GPU driver → kernel
// (ADD AD-001, Implementation Note #1).
var rebootClassRules = []classRule{
	// 1. Exclusions: GPU-looking names that are NOT driver packages
	// (container-toolkit user-space plumbing, firmware blobs). Upgrading
	// these creates no kernel/user-space mismatch — explicit, tested
	// decisions per NFR-006.
	{exact("linux-firmware"), ""},
	{prefix("libnvidia-container"), ""},
	{prefix("nvidia-container-toolkit"), ""},
	{exact("nvidia-docker2"), ""},

	// 2. GPU driver patterns (FR-002 apt, FR-003 dnf).
	{prefix("nvidia-driver"), ClassGPUDriver},
	{prefix("nvidia-dkms"), ClassGPUDriver},
	{prefix("nvidia-kernel"), ClassGPUDriver},
	{prefix("libnvidia-"), ClassGPUDriver},
	{prefix("nvidia-utils"), ClassGPUDriver},
	{prefix("xserver-xorg-video-nvidia"), ClassGPUDriver},
	{prefix("akmod-nvidia"), ClassGPUDriver},
	{prefix("kmod-nvidia"), ClassGPUDriver},
	{prefix("xorg-x11-drv-nvidia"), ClassGPUDriver},
	{prefix("nvidia-kmod"), ClassGPUDriver},
	{prefix("amdgpu"), ClassGPUDriver}, // amdgpu-dkms, amdgpu-pro, dnf amdgpu stack
	{exact("rock-dkms"), ClassGPUDriver},
	{prefix("rocm-dkms"), ClassGPUDriver},
	// ROCm kernel-module packages: rocm* names that end in -dkms or contain
	// "kmod". Plain rocm-* user-space libraries stay standard.
	{func(name string) bool {
		return strings.HasPrefix(name, "rocm") &&
			(strings.HasSuffix(name, "-dkms") || strings.Contains(name, "kmod"))
	}, ClassGPUDriver},

	// 3. Kernel patterns (FR-004 apt, FR-005 dnf).
	{prefix("linux-image-"), ClassKernel},
	{prefix("linux-headers-"), ClassKernel},
	{prefix("linux-modules-"), ClassKernel},
	{prefix("linux-generic"), ClassKernel}, // flavor metapackages
	{exact("kernel"), ClassKernel},
	{exact("kernel-core"), ClassKernel},
	{exact("kernel-headers"), ClassKernel},
	{exact("kernel-devel"), ClassKernel},
	{prefix("kernel-modules"), ClassKernel},
	// Any remaining *-dkms package: GPU dkms names were already claimed by
	// rule block 2, so only non-GPU kernel-module builds land here (FR-006).
	{suffix("-dkms"), ClassKernel},
}

// ClassifyPackage returns ClassGPUDriver, ClassKernel, or "" (standard) for
// a package name. The same table serves apt and dnf (FR-007).
func ClassifyPackage(name string) string {
	for _, r := range rebootClassRules {
		if r.match(name) {
			return r.class
		}
	}
	return ""
}

// SelectPrimaryKernel picks the kernel package whose NewVer becomes
// UpdateInfo.PendingKernelVersion (PATCH-GATE AD-002). Among pending updates
// classified ClassKernel, the versioned image package wins over metapackages
// and headers; ties break on the lexicographically smallest name so the
// choice is deterministic across cycles. Returns empty strings when no
// kernel-class update is pending.
func SelectPrimaryKernel(updates []models.PendingUpdate) (name, version string) {
	bestRank := -1
	for _, u := range updates {
		if ClassifyPackage(u.Name) != ClassKernel {
			continue
		}
		rank := kernelRank(u.Name)
		if bestRank == -1 || rank < bestRank || (rank == bestRank && u.Name < name) {
			bestRank = rank
			name = u.Name
			version = u.NewVer
		}
	}
	return name, version
}

// kernelRank orders kernel-class package names by how well they represent
// the actual pending kernel version (AD-002 rank table; lower wins).
func kernelRank(name string) int {
	switch {
	case name == "kernel":
		return 0
	case strings.HasPrefix(name, "linux-image-") && !strings.HasPrefix(name, "linux-image-generic") && containsDigit(name):
		return 0
	case name == "kernel-core":
		return 1
	case strings.HasPrefix(name, "linux-image-generic"), strings.HasPrefix(name, "linux-generic"):
		return 1
	default:
		return 2
	}
}

func containsDigit(s string) bool {
	return strings.ContainsAny(s, "0123456789")
}

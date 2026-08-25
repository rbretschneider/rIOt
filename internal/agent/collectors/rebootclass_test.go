package collectors

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// [AC-001] apt GPU driver packages classified gpu_driver (GPU precedence over kernel).
func TestClassifyPackage_AC001_AptGPUDrivers(t *testing.T) {
	for _, name := range []string{
		"nvidia-driver-550",
		"libnvidia-compute-550",
		"nvidia-dkms-550",
		"rock-dkms",
	} {
		assert.Equal(t, ClassGPUDriver, ClassifyPackage(name), "[AC-001] %s must classify gpu_driver", name)
	}

	t.Run("[AC-001] nvidia-dkms-550 is gpu_driver, not kernel (GPU precedence)", func(t *testing.T) {
		assert.Equal(t, ClassGPUDriver, ClassifyPackage("nvidia-dkms-550"))
		assert.NotEqual(t, ClassKernel, ClassifyPackage("nvidia-dkms-550"))
	})
}

// [AC-002] apt kernel packages classified kernel (incl. the -dkms suffix rule).
func TestClassifyPackage_AC002_AptKernelPackages(t *testing.T) {
	for _, name := range []string{
		"linux-image-6.8.0-45-generic",
		"linux-headers-6.8.0-45-generic",
		"linux-modules-6.8.0-45-generic",
		"zfs-dkms",
	} {
		assert.Equal(t, ClassKernel, ClassifyPackage(name), "[AC-002] %s must classify kernel", name)
	}
	// Flavor metapackages (FR-004).
	assert.Equal(t, ClassKernel, ClassifyPackage("linux-generic"))
	assert.Equal(t, ClassKernel, ClassifyPackage("linux-image-generic"))
}

// [AC-003] dnf GPU driver packages classified gpu_driver via the same table.
func TestClassifyPackage_AC003_DnfGPUDrivers(t *testing.T) {
	for _, name := range []string{
		"akmod-nvidia",
		"xorg-x11-drv-nvidia-cuda",
		"kmod-nvidia-latest-dkms",
	} {
		assert.Equal(t, ClassGPUDriver, ClassifyPackage(name), "[AC-003] %s must classify gpu_driver", name)
	}
}

// [AC-004] dnf kernel packages classified kernel.
func TestClassifyPackage_AC004_DnfKernelPackages(t *testing.T) {
	for _, name := range []string{
		"kernel",
		"kernel-core",
		"kernel-modules-extra",
		"kernel-devel",
	} {
		assert.Equal(t, ClassKernel, ClassifyPackage(name), "[AC-004] %s must classify kernel", name)
	}
	assert.Equal(t, ClassKernel, ClassifyPackage("kernel-headers"))
}

// [AC-005] Standard packages remain unclassified — including the explicit
// exclusion decisions (linux-firmware, libnvidia-container*, toolkit) per NFR-006.
func TestClassifyPackage_AC005_StandardPackagesUnclassified(t *testing.T) {
	for _, name := range []string{
		"curl",
		"openssl",
		"linux-firmware",
		"docker-ce",
		"libnvidia-container-tools",
		"libnvidia-container1",
		"nvidia-container-toolkit",
		"nvidia-docker2",
		"rocm-smi-lib", // ROCm user-space stays standard
	} {
		assert.Equal(t, "", ClassifyPackage(name), "[AC-005] %s must remain unclassified", name)
	}
}

// [AC-006] Primary kernel selection feeds PendingKernelVersion deterministically.
func TestSelectPrimaryKernel_AC006(t *testing.T) {
	t.Run("[AC-006] versioned linux-image wins over headers and metapackages", func(t *testing.T) {
		name, ver := SelectPrimaryKernel([]models.PendingUpdate{
			{Name: "linux-headers-6.8.0-45-generic", NewVer: "6.8.0-45.45"},
			{Name: "linux-image-generic", NewVer: "6.8.0-45.45+1"},
			{Name: "linux-image-6.8.0-45-generic", NewVer: "6.8.0-45.45"},
		})
		assert.Equal(t, "linux-image-6.8.0-45-generic", name)
		assert.Equal(t, "6.8.0-45.45", ver)
	})

	t.Run("[AC-006] dnf exact kernel wins over kernel-core", func(t *testing.T) {
		name, ver := SelectPrimaryKernel([]models.PendingUpdate{
			{Name: "kernel-core", NewVer: "6.7.5-200.fc39"},
			{Name: "kernel", NewVer: "6.7.5-200.fc39"},
			{Name: "kernel-devel", NewVer: "6.7.5-200.fc39"},
		})
		assert.Equal(t, "kernel", name)
		assert.Equal(t, "6.7.5-200.fc39", ver)
	})

	t.Run("[AC-006] no kernel-class pending returns empty", func(t *testing.T) {
		name, ver := SelectPrimaryKernel([]models.PendingUpdate{
			{Name: "curl", NewVer: "8.5.0"},
			{Name: "nvidia-driver-550", NewVer: "550.90"},
		})
		assert.Empty(t, name)
		assert.Empty(t, ver)
	})

	t.Run("[AC-006] equal-rank ties break lexicographically", func(t *testing.T) {
		name, _ := SelectPrimaryKernel([]models.PendingUpdate{
			{Name: "linux-headers-b", NewVer: "2"},
			{Name: "linux-headers-a", NewVer: "1"},
		})
		assert.Equal(t, "linux-headers-a", name)
	})
}

// [SEC-AC-003] package-name charset allowlist: legal dpkg/rpm names accepted,
// metacharacter and option-injection shapes rejected (SEC-PATCH-GATE-003).
func TestValidPackageName_SECAC003(t *testing.T) {
	accepted := []string{
		"nvidia-driver-550",
		"libc6:amd64",
		"kernel-core",
		"zfs-dkms",
		"1password",
		"libstdc++6",
		"python3.12",
		"tzdata~beta",
		"gcc_multilib",
	}
	for _, name := range accepted {
		assert.True(t, ValidPackageName(name), "[SEC-AC-003] %q must be accepted", name)
	}

	rejected := []string{
		"evil,pkg",
		"bad\nname",
		"[main]x",
		"pkg]x",
		"pkg=value",
		"pkg#comment",
		"pkg name",
		"-o",
		"--allow-downgrades",
		"-Dpkg::Options",
		"",
		" leading-space",
		"trailing-space ",
	}
	for _, name := range rejected {
		assert.False(t, ValidPackageName(name), "[SEC-AC-003] %q must be rejected", name)
	}
}

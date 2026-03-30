package collectors

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// twoGPUOutput simulates nvidia-smi output for a 2-GPU host.
const twoGPUOutput = `0, NVIDIA GeForce RTX 3090, GPU-aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb, 72, 65, 85, 42, 10240, 24576, 285.50, 350.00, 00000000:01:00.0
1, NVIDIA GeForce RTX 3080, GPU-cccccccc-4444-5555-6666-dddddddddddd, 68, 58, 72, 35, 8192, 10240, 215.30, 320.00, 00000000:02:00.0`

// singleGPUWithUnsupported simulates a GPU where fan speed reports [Not Supported].
const singleGPUWithUnsupported = `0, NVIDIA Tesla T4, GPU-eeeeeeee-7777-8888-9999-ffffffffffff, 45, [Not Supported], 30, 20, 4096, 16384, 55.00, 70.00, 00000000:03:00.0`

// fourGPUOutput simulates a 4-GPU host with distinct indices and UUIDs.
const fourGPUOutput = `0, NVIDIA A100, GPU-00000000-0000-0000-0000-000000000001, 60, 40, 90, 70, 20480, 40960, 300.00, 400.00, 00000000:01:00.0
1, NVIDIA A100, GPU-00000000-0000-0000-0000-000000000002, 61, 41, 88, 68, 20480, 40960, 298.00, 400.00, 00000000:02:00.0
2, NVIDIA A100, GPU-00000000-0000-0000-0000-000000000003, 59, 39, 85, 65, 20480, 40960, 295.00, 400.00, 00000000:03:00.0
3, NVIDIA A100, GPU-00000000-0000-0000-0000-000000000004, 62, 42, 91, 72, 20480, 40960, 302.00, 400.00, 00000000:04:00.0`

// gpuWithNAFields simulates a GPU reporting N/A for some fields.
const gpuWithNAFields = `0, NVIDIA GeForce RTX 3060, GPU-12345678-abcd-efgh-ijkl-123456789012, 55, N/A, 50, N/A, 6144, 12288, N/A, 170.00, 00000000:01:00.0`

func TestGPUCollectorName(t *testing.T) {
	c := &GPUCollector{}
	assert.Equal(t, "gpu", c.Name(), "collector must be named 'gpu'")
}

// [AC-012] gpu collector must not run unless explicitly added to collectors.enabled.
// This is enforced at the registry level (FilterEnabled), not the collector itself.
// We verify the collector registers under the name "gpu" so FilterEnabled can include it.
func TestGPUCollector_RegistrationName(t *testing.T) {
	r := NewRegistry()
	r.RegisterDefaultsWithDocker(DockerOptions{CollectStats: false})

	names := make([]string, 0)
	for _, c := range r.Collectors() {
		names = append(names, c.Name())
	}

	assert.Contains(t, names, "gpu", "[AC-012] gpu must be registered so FilterEnabled can include it")
}

// [AC-012] FilterEnabled must exclude gpu when it is not in the enabled list.
func TestGPUCollector_FilterEnabled_ExcludesGPUByDefault(t *testing.T) {
	r := NewRegistry()
	r.RegisterDefaultsWithDocker(DockerOptions{CollectStats: false})

	// Simulate a default config that does not list "gpu"
	r.FilterEnabled([]string{"cpu", "memory", "disk"})

	for _, c := range r.Collectors() {
		assert.NotEqual(t, "gpu", c.Name(), "[AC-012] gpu must not run when not in enabled list")
	}
}

// [AC-002] Returns empty result with no error when not on Linux.
// On all platforms, the non-Linux stub (or LookPath failure) returns empty.
func TestGPUCollector_ReturnsEmptyWhenNoNvidiaSMI(t *testing.T) {
	// On Windows (test environment), nvidia-smi is not present.
	// The Linux-build GPUCollector also returns empty on LookPath failure,
	// and the non-Linux stub always returns empty.
	c := &GPUCollector{}
	result, err := c.Collect(context.Background())
	require.NoError(t, err, "[AC-002] collector must not return an error when nvidia-smi is absent")
	require.NotNil(t, result, "[AC-002] result must not be nil")
}

// [AC-001] parseNvidiaSMIOutput populates all fields for a 2-GPU output.
func TestParseNvidiaSMIOutput_TwoGPUs_AllFieldsPopulated(t *testing.T) {
	gpus := parseNvidiaSMIOutput(twoGPUOutput)

	require.Len(t, gpus, 2, "[AC-001] must parse exactly 2 GPU entries")

	g0 := gpus[0]
	assert.Equal(t, 0, g0.Index, "[AC-001] GPU 0 index")
	assert.Equal(t, "NVIDIA GeForce RTX 3090", g0.Name, "[AC-001] GPU 0 name")
	assert.Equal(t, "GPU-aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb", g0.UUID, "[AC-001] GPU 0 UUID")
	assert.Equal(t, "00000000:01:00.0", g0.PCIBusID, "[AC-001] GPU 0 PCI bus ID")
	require.NotNil(t, g0.TemperatureC, "[AC-001] GPU 0 temperature must be populated")
	assert.Equal(t, 72, *g0.TemperatureC, "[AC-001] GPU 0 temperature value")
	require.NotNil(t, g0.FanSpeedPercent)
	assert.Equal(t, 65, *g0.FanSpeedPercent)
	require.NotNil(t, g0.UtilizationPct)
	assert.Equal(t, 85, *g0.UtilizationPct)
	require.NotNil(t, g0.MemUtilPct)
	assert.Equal(t, 42, *g0.MemUtilPct)
	require.NotNil(t, g0.MemUsedMiB)
	assert.Equal(t, 10240, *g0.MemUsedMiB)
	require.NotNil(t, g0.MemTotalMiB)
	assert.Equal(t, 24576, *g0.MemTotalMiB)
	require.NotNil(t, g0.PowerDrawW)
	assert.InDelta(t, 285.50, *g0.PowerDrawW, 0.001)
	require.NotNil(t, g0.PowerLimitW)
	assert.InDelta(t, 350.00, *g0.PowerLimitW, 0.001)

	g1 := gpus[1]
	assert.Equal(t, 1, g1.Index, "[AC-001] GPU 1 index")
	assert.NotEqual(t, g0.UUID, g1.UUID, "[AC-001] GPUs must have distinct UUIDs")
}

// [AC-003] Four distinct GPUs are parsed with distinct indices and UUIDs.
func TestParseNvidiaSMIOutput_FourGPUs_DistinctIndicesAndUUIDs(t *testing.T) {
	gpus := parseNvidiaSMIOutput(fourGPUOutput)

	require.Len(t, gpus, 4, "[AC-003] must parse exactly 4 GPU entries")

	indices := make(map[int]bool)
	uuids := make(map[string]bool)
	for _, g := range gpus {
		indices[g.Index] = true
		uuids[g.UUID] = true
	}

	assert.Len(t, indices, 4, "[AC-003] all GPU indices must be distinct")
	assert.Len(t, uuids, 4, "[AC-003] all GPU UUIDs must be distinct")

	assert.True(t, indices[0] && indices[1] && indices[2] && indices[3],
		"[AC-003] indices must be 0, 1, 2, 3")
}

// [AC-004] [Not Supported] fan speed is parsed as nil; other fields populated.
func TestParseNvidiaSMIOutput_NotSupportedField_IsNil(t *testing.T) {
	gpus := parseNvidiaSMIOutput(singleGPUWithUnsupported)

	require.Len(t, gpus, 1, "[AC-004] must parse 1 GPU entry")

	g := gpus[0]
	assert.Nil(t, g.FanSpeedPercent, "[AC-004] [Not Supported] fan speed must be nil")

	// All other supported fields must be populated
	assert.Equal(t, "NVIDIA Tesla T4", g.Name, "[AC-004] name must still be populated")
	require.NotNil(t, g.TemperatureC, "[AC-004] temperature must still be populated")
	assert.Equal(t, 45, *g.TemperatureC)
	require.NotNil(t, g.UtilizationPct)
	require.NotNil(t, g.MemUsedMiB)
	require.NotNil(t, g.MemTotalMiB)
	require.NotNil(t, g.PowerDrawW)
	require.NotNil(t, g.PowerLimitW)
}

// [AC-004] N/A fields are also parsed as nil.
func TestParseNvidiaSMIOutput_NAFields_AreNil(t *testing.T) {
	gpus := parseNvidiaSMIOutput(gpuWithNAFields)

	require.Len(t, gpus, 1)
	g := gpus[0]

	assert.Nil(t, g.FanSpeedPercent, "[AC-004] N/A fan speed must be nil")
	assert.Nil(t, g.MemUtilPct, "[AC-004] N/A mem util must be nil")
	assert.Nil(t, g.PowerDrawW, "[AC-004] N/A power draw must be nil")

	// Non-absent fields must be populated
	require.NotNil(t, g.TemperatureC)
	require.NotNil(t, g.UtilizationPct)
	require.NotNil(t, g.MemUsedMiB)
	require.NotNil(t, g.MemTotalMiB)
	require.NotNil(t, g.PowerLimitW)
}

// Empty nvidia-smi output produces an empty slice, no errors.
func TestParseNvidiaSMIOutput_EmptyOutput_ReturnsEmpty(t *testing.T) {
	gpus := parseNvidiaSMIOutput("")
	assert.Empty(t, gpus, "empty output must produce an empty GPU slice")
}

// Lines with wrong field count are skipped; valid lines are still parsed.
func TestParseNvidiaSMIOutput_MalformedLine_IsSkipped(t *testing.T) {
	malformed := "0, NVIDIA Test GPU, GPU-uuid, 70, 50\n" + // only 5 fields — skip
		"1, NVIDIA Good GPU, GPU-ffffffff-0000-0000-0000-111111111111, 65, 40, 80, 30, 4096, 8192, 100.00, 150.00, 00000000:04:00.0"

	gpus := parseNvidiaSMIOutput(malformed)

	require.Len(t, gpus, 1, "malformed line must be skipped; valid line must be parsed")
	assert.Equal(t, 1, gpus[0].Index)
	assert.Equal(t, "NVIDIA Good GPU", gpus[0].Name)
}

// Garbage numeric field is silently treated as nil (defensive parsing per AD-004).
func TestParseNvidiaSMIOutput_GarbageNumericField_IsNil(t *testing.T) {
	garbage := `0, NVIDIA Test, GPU-test-uuid, GARBAGE, 50, 80, 30, 4096, 8192, 100.00, 150.00, 00000000:01:00.0`

	gpus := parseNvidiaSMIOutput(garbage)

	require.Len(t, gpus, 1)
	assert.Nil(t, gpus[0].TemperatureC, "garbage temperature field must be nil")
	require.NotNil(t, gpus[0].FanSpeedPercent, "other fields must still parse")
}

func TestIsAbsent(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"[Not Supported]", true},
		{"N/A", true},
		{"72", false},
		{"285.50", false},
		{"0", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, isAbsent(tt.input))
		})
	}
}

func TestParseIntField(t *testing.T) {
	tests := []struct {
		input string
		want  *int
	}{
		{"72", intPtr(72)},
		{"0", intPtr(0)},
		{"", nil},
		{"[Not Supported]", nil},
		{"N/A", nil},
		{"abc", nil},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseIntField(tt.input)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, *tt.want, *got)
			}
		})
	}
}

func TestParseFloat64Field(t *testing.T) {
	tests := []struct {
		input string
		want  *float64
	}{
		{"285.50", float64Ptr(285.50)},
		{"0.0", float64Ptr(0.0)},
		{"", nil},
		{"[Not Supported]", nil},
		{"N/A", nil},
		{"abc", nil},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseFloat64Field(tt.input)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.InDelta(t, *tt.want, *got, 0.001)
			}
		})
	}
}

// helpers

func intPtr(v int) *int          { return &v }
func float64Ptr(v float64) *float64 { return &v }

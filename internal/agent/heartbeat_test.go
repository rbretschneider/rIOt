package agent

import (
	"encoding/json"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// isLoopbackName helper
// ---------------------------------------------------------------------------

func TestIsLoopbackName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"lo", true},
		{"lo0", true},
		{"Loopback Pseudo-Interface 1", true},
		{"Loopback", true},
		{"eth0", false},
		{"lo-fancy", false},
		{"wlan0", false},
		{"docker0", false},
		{"br0", false},
		{"wg0", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isLoopbackName(tc.name),
				"isLoopbackName(%q) mismatch", tc.name)
		})
	}
}

// ---------------------------------------------------------------------------
// computeNetRates — [AC-002] cold start
// ---------------------------------------------------------------------------

func TestComputeNetRates_ColdStart(t *testing.T) {
	t.Run("[AC-002] cold start with nil prev returns zero rates", func(t *testing.T) {
		curr := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 1000, BytesSent: 2000},
		}
		rx, tx := computeNetRates(nil, curr, 10.0)
		assert.Equal(t, float64(0), rx, "rx must be 0 on cold start")
		assert.Equal(t, float64(0), tx, "tx must be 0 on cold start")
	})
}

// ---------------------------------------------------------------------------
// computeNetRates — [AC-003] normal interval
// ---------------------------------------------------------------------------

func TestComputeNetRates_NormalInterval(t *testing.T) {
	t.Run("[AC-003] normal interval produces R/T rate", func(t *testing.T) {
		prev := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 1000, BytesSent: 2000},
		}
		curr := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 11000, BytesSent: 22000},
		}
		rx, tx := computeNetRates(prev, curr, 10.0)
		// (11000 - 1000) / 10 = 1000 B/s
		assert.InDelta(t, 1000.0, rx, 0.001, "rx rate must equal delta/elapsed")
		// (22000 - 2000) / 10 = 2000 B/s
		assert.InDelta(t, 2000.0, tx, 0.001, "tx rate must equal delta/elapsed")
	})

	t.Run("[AC-003] normal interval sums multiple interfaces", func(t *testing.T) {
		prev := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 0, BytesSent: 0},
			"eth1": {BytesRecv: 0, BytesSent: 0},
		}
		curr := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 5000, BytesSent: 3000},
			"eth1": {BytesRecv: 5000, BytesSent: 3000},
		}
		rx, tx := computeNetRates(prev, curr, 5.0)
		// (5000+5000)/5 = 2000
		assert.InDelta(t, 2000.0, rx, 0.001, "rx must sum across interfaces")
		// (3000+3000)/5 = 1200
		assert.InDelta(t, 1200.0, tx, 0.001, "tx must sum across interfaces")
	})
}

// ---------------------------------------------------------------------------
// computeNetRates — [AC-004] counter rollover
// ---------------------------------------------------------------------------

func TestComputeNetRates_Rollover(t *testing.T) {
	t.Run("[AC-004] counter rollover contributes zero for that interface", func(t *testing.T) {
		prev := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 1000, BytesSent: 2000},
		}
		// rx went backwards (rollover / reset), tx went backwards too
		curr := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 100, BytesSent: 50},
		}
		rx, tx := computeNetRates(prev, curr, 10.0)
		assert.GreaterOrEqual(t, rx, float64(0), "rx must be non-negative after rollover")
		assert.GreaterOrEqual(t, tx, float64(0), "tx must be non-negative after rollover")
		assert.Equal(t, float64(0), rx, "rollover rx contribution must be zero")
		assert.Equal(t, float64(0), tx, "rollover tx contribution must be zero")
	})

	t.Run("[AC-004] rollover on one interface does not affect another", func(t *testing.T) {
		prev := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 1000, BytesSent: 1000},
			"eth1": {BytesRecv: 100, BytesSent: 100},
		}
		curr := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 6000, BytesSent: 6000}, // normal: +5000
			"eth1": {BytesRecv: 10, BytesSent: 10},      // rollover: contributes 0
		}
		rx, tx := computeNetRates(prev, curr, 5.0)
		assert.InDelta(t, 1000.0, rx, 0.001, "only non-rolled interface contributes")
		assert.InDelta(t, 1000.0, tx, 0.001, "only non-rolled interface contributes")
	})
}

// ---------------------------------------------------------------------------
// computeNetRates — [AC-005] interface added mid-run
// ---------------------------------------------------------------------------

func TestComputeNetRates_InterfaceAdded(t *testing.T) {
	t.Run("[AC-005] interface added mid-run contributes zero for that interval", func(t *testing.T) {
		prev := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 0, BytesSent: 0},
		}
		// docker0 appeared since last heartbeat
		curr := map[string]netIOSnapshot{
			"eth0":    {BytesRecv: 10000, BytesSent: 5000},
			"docker0": {BytesRecv: 99999, BytesSent: 99999}, // new — must not contribute
		}
		rx, tx := computeNetRates(prev, curr, 10.0)
		// Only eth0 contributes: 10000/10 = 1000, 5000/10 = 500
		assert.InDelta(t, 1000.0, rx, 0.001, "new interface must not contribute rx this interval")
		assert.InDelta(t, 500.0, tx, 0.001, "new interface must not contribute tx this interval")
	})
}

// ---------------------------------------------------------------------------
// computeNetRates — [AC-006] interface removed mid-run
// ---------------------------------------------------------------------------

func TestComputeNetRates_InterfaceRemoved(t *testing.T) {
	t.Run("[AC-006] interface removed mid-run contributes zero for that interval", func(t *testing.T) {
		prev := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 0, BytesSent: 0},
			"wg0":  {BytesRecv: 50000, BytesSent: 50000}, // wg0 will disappear
		}
		// wg0 gone from current snapshot
		curr := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 10000, BytesSent: 5000},
		}
		rx, tx := computeNetRates(prev, curr, 10.0)
		// Only eth0 contributes: 1000 / 500
		assert.InDelta(t, 1000.0, rx, 0.001, "removed interface must not appear in rx sum")
		assert.InDelta(t, 500.0, tx, 0.001, "removed interface must not appear in tx sum")
	})
}

// ---------------------------------------------------------------------------
// isLoopbackName + computeNetRates — [AC-007] loopback excluded
// ---------------------------------------------------------------------------

func TestComputeNetRates_LoopbackExcluded(t *testing.T) {
	t.Run("[AC-007] loopback excluded from sum", func(t *testing.T) {
		// After filtering loopback in sendHeartbeat, lo and lo0 never reach
		// computeNetRates. Verify the outer filter is coherent by testing that
		// a curr map built WITHOUT loopback yields the correct sum.
		prev := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 0, BytesSent: 0},
			// lo, lo0 intentionally absent — filtered before computeNetRates
		}
		curr := map[string]netIOSnapshot{
			"eth0": {BytesRecv: 10000, BytesSent: 5000},
			// lo and lo0 filtered out by isLoopbackName before building curr
		}
		rx, tx := computeNetRates(prev, curr, 10.0)
		assert.InDelta(t, 1000.0, rx, 0.001, "only eth0 contributes rx")
		assert.InDelta(t, 500.0, tx, 0.001, "only eth0 contributes tx")
	})

	t.Run("[AC-007] direct loopback name check covers lo, lo0, Windows form", func(t *testing.T) {
		assert.True(t, isLoopbackName("lo"), "lo must be loopback")
		assert.True(t, isLoopbackName("lo0"), "lo0 must be loopback")
		assert.True(t, isLoopbackName("Loopback Pseudo-Interface 1"), "Windows Loopback must match")
		assert.False(t, isLoopbackName("eth0"), "eth0 must not be loopback")
		assert.False(t, isLoopbackName("lo-fancy"), "lo-fancy must not be loopback (renamed interface)")
	})
}

// ---------------------------------------------------------------------------
// computeNetRates — [AC-008] non-loopback interfaces of all kinds are summed
// ---------------------------------------------------------------------------

func TestComputeNetRates_AllNonLoopbackIncluded(t *testing.T) {
	t.Run("[AC-008] non-loopback interfaces of all kinds are summed", func(t *testing.T) {
		prev := map[string]netIOSnapshot{
			"eth0":    {BytesRecv: 0, BytesSent: 0},
			"br0":     {BytesRecv: 0, BytesSent: 0},
			"docker0": {BytesRecv: 0, BytesSent: 0},
			"wg0":     {BytesRecv: 0, BytesSent: 0},
			"wlan0":   {BytesRecv: 0, BytesSent: 0},
		}
		curr := map[string]netIOSnapshot{
			"eth0":    {BytesRecv: 1000, BytesSent: 1000},
			"br0":     {BytesRecv: 1000, BytesSent: 1000},
			"docker0": {BytesRecv: 1000, BytesSent: 1000},
			"wg0":     {BytesRecv: 1000, BytesSent: 1000},
			"wlan0":   {BytesRecv: 1000, BytesSent: 1000},
		}
		rx, tx := computeNetRates(prev, curr, 1.0)
		// 5 interfaces × 1000 B = 5000 total
		assert.InDelta(t, 5000.0, rx, 0.001, "all five non-loopback interfaces must contribute rx")
		assert.InDelta(t, 5000.0, tx, 0.001, "all five non-loopback interfaces must contribute tx")
	})
}

// ---------------------------------------------------------------------------
// computeNetRates — [AC-009] zero or negative elapsed time
// ---------------------------------------------------------------------------

func TestComputeNetRates_ZeroOrNegativeElapsed(t *testing.T) {
	prev := map[string]netIOSnapshot{
		"eth0": {BytesRecv: 1000, BytesSent: 2000},
	}
	curr := map[string]netIOSnapshot{
		"eth0": {BytesRecv: 2000, BytesSent: 3000},
	}

	t.Run("[AC-009] zero elapsed time emits zero, no divide-by-zero panic", func(t *testing.T) {
		rx, tx := computeNetRates(prev, curr, 0.0)
		assert.Equal(t, float64(0), rx, "rx must be 0 when elapsed == 0")
		assert.Equal(t, float64(0), tx, "tx must be 0 when elapsed == 0")
	})

	t.Run("[AC-009] negative elapsed time (clock jumped backwards) emits zero", func(t *testing.T) {
		rx, tx := computeNetRates(prev, curr, -1.0)
		assert.Equal(t, float64(0), rx, "rx must be 0 when elapsed < 0")
		assert.Equal(t, float64(0), tx, "tx must be 0 when elapsed < 0")
	})
}

// ---------------------------------------------------------------------------
// HeartbeatData JSON round-trip — [AC-001] field names and omitempty
// ---------------------------------------------------------------------------

func TestHeartbeatDataFieldNames(t *testing.T) {
	t.Run("[AC-001] non-zero net rate fields serialize to net_rx_bytes_sec / net_tx_bytes_sec", func(t *testing.T) {
		hb := models.HeartbeatData{
			NetRxBytesPerSec: 1234.5,
			NetTxBytesPerSec: 5678.9,
		}
		b, err := json.Marshal(hb)
		assert.NoError(t, err)
		var m map[string]interface{}
		assert.NoError(t, json.Unmarshal(b, &m))
		assert.Contains(t, m, "net_rx_bytes_sec", "JSON must contain key net_rx_bytes_sec")
		assert.Contains(t, m, "net_tx_bytes_sec", "JSON must contain key net_tx_bytes_sec")
		assert.NotContains(t, m, "net_rx_bytes_per_sec", "FRD spelling must NOT appear in JSON (AD-001)")
		assert.NotContains(t, m, "net_tx_bytes_per_sec", "FRD spelling must NOT appear in JSON (AD-001)")
		assert.InDelta(t, 1234.5, m["net_rx_bytes_sec"], 0.001)
		assert.InDelta(t, 5678.9, m["net_tx_bytes_sec"], 0.001)
	})

	t.Run("[AC-001] omitempty: zero values omit both keys from JSON", func(t *testing.T) {
		hb := models.HeartbeatData{}
		b, err := json.Marshal(hb)
		assert.NoError(t, err)
		var m map[string]interface{}
		assert.NoError(t, json.Unmarshal(b, &m))
		assert.NotContains(t, m, "net_rx_bytes_sec", "omitempty must suppress zero rx key")
		assert.NotContains(t, m, "net_tx_bytes_sec", "omitempty must suppress zero tx key")
	})
}

// ---------------------------------------------------------------------------
// Payload size — [AC-020]
// ---------------------------------------------------------------------------

func TestHeartbeatPayloadSizeDelta(t *testing.T) {
	t.Run("[AC-020] payload size delta with new fields is within 64 bytes", func(t *testing.T) {
		// NFR-002 specifies ≤64 bytes for "realistic rates". The ADD uses
		// 1234567.89 as the realistic worst-case value (7 digits before decimal,
		// matching ~1.2 MB/s). Very high rates (e.g. 10 GB/s = 10_000_000_000)
		// would exceed 64 bytes, but the ADD explicitly qualifies "realistic".
		// See ADD §11 Performance Considerations for the NFR-002 analysis.
		base := models.HeartbeatData{
			Uptime:          3600,
			CPUPercent:      45.2,
			MemPercent:      61.3,
			LoadAvg1m:       1.5,
			DiskRootPercent: 72.1,
		}
		withNet := base
		// Use the ADD's own example values for "worst-case realistic rates"
		withNet.NetRxBytesPerSec = 1234567.89
		withNet.NetTxBytesPerSec = 1234567.89

		bBase, err := json.Marshal(base)
		assert.NoError(t, err)
		bWithNet, err := json.Marshal(withNet)
		assert.NoError(t, err)

		delta := len(bWithNet) - len(bBase)
		assert.LessOrEqual(t, delta, 64,
			"payload size increase (%d bytes) must not exceed 64 bytes (NFR-002)", delta)
	})
}

// ---------------------------------------------------------------------------
// JSON decode — [AC-010] old agent missing net fields decodes to zero
// ---------------------------------------------------------------------------

func TestHeartbeatDataMissingFieldsDecodeToZero(t *testing.T) {
	t.Run("[AC-010] heartbeat without net fields decodes both to zero", func(t *testing.T) {
		payload := `{"uptime":3600,"cpu_percent":45,"mem_percent":60,"load_avg_1m":1.2,"disk_root_percent":70}`
		var hb models.HeartbeatData
		err := json.Unmarshal([]byte(payload), &hb)
		assert.NoError(t, err)
		assert.Equal(t, float64(0), hb.NetRxBytesPerSec, "missing net_rx_bytes_sec must decode to 0")
		assert.Equal(t, float64(0), hb.NetTxBytesPerSec, "missing net_tx_bytes_sec must decode to 0")
	})
}

// ---------------------------------------------------------------------------
// JSON decode — [AC-011] updated agent's extra fields ignored by old struct
// ---------------------------------------------------------------------------

func TestNewAgentFieldsIgnoredByOldStruct(t *testing.T) {
	t.Run("[AC-011] new agent fields are silently ignored by a struct without those fields", func(t *testing.T) {
		// Simulate the old server struct that lacks the new fields
		type oldHeartbeatData struct {
			Uptime          uint64  `json:"uptime"`
			CPUPercent      float64 `json:"cpu_percent"`
			MemPercent      float64 `json:"mem_percent"`
			LoadAvg1m       float64 `json:"load_avg_1m"`
			DiskRootPercent float64 `json:"disk_root_percent"`
		}

		// Simulate a new-agent JSON payload (includes net fields)
		newAgentPayload := `{
			"uptime":3600,
			"cpu_percent":45,
			"mem_percent":60,
			"load_avg_1m":1.2,
			"disk_root_percent":70,
			"net_rx_bytes_sec":1234.5,
			"net_tx_bytes_sec":5678.9
		}`

		var old oldHeartbeatData
		err := json.Unmarshal([]byte(newAgentPayload), &old)
		assert.NoError(t, err, "unknown fields must not cause a decode error")
		assert.Equal(t, float64(45), old.CPUPercent, "known field must still decode correctly")
	})
}

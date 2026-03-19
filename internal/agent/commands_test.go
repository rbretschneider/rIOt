package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncateOutputSmart_Short(t *testing.T) {
	input := "short output"
	result := TruncateOutputSmart(input, 1024)
	assert.Equal(t, input, result)
}

func TestTruncateOutputSmart_ExactLimit(t *testing.T) {
	input := strings.Repeat("x", 256*1024)
	result := TruncateOutputSmart(input, 256*1024)
	assert.Equal(t, input, result)
}

func TestTruncateOutputSmart_Truncated(t *testing.T) {
	input := strings.Repeat("A", 100*1024) + strings.Repeat("B", 100*1024) + strings.Repeat("C", 100*1024)
	result := TruncateOutputSmart(input, 256*1024)

	// Result should be smaller than original
	assert.Less(t, len(result), len(input))
	// Should contain the truncation marker
	assert.Contains(t, result, "... [truncated] ...")
	// Should start with the head of the input
	assert.True(t, strings.HasPrefix(result, strings.Repeat("A", 64*1024)))
	// Should end with the tail of the input
	assert.True(t, strings.HasSuffix(result, strings.Repeat("C", 64*1024)))
}

func TestTruncateOutputSmart_Empty(t *testing.T) {
	result := TruncateOutputSmart("", 1024)
	assert.Equal(t, "", result)
}

func TestTruncateOutputSmart_SmallLimit(t *testing.T) {
	input := strings.Repeat("x", 100)
	result := TruncateOutputSmart(input, 50)
	assert.LessOrEqual(t, len(result), 100) // head(25) + marker + tail(25)
	assert.Contains(t, result, "... [truncated] ...")
}

func TestParseAptSummary_UpToDate(t *testing.T) {
	output := `Reading package lists...
Building dependency tree...
Reading state information...
0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.`
	result := parseAptSummary(output)
	assert.Equal(t, "System is up to date", result)
}

func TestParseAptSummary_WithUpgrades(t *testing.T) {
	output := `12 upgraded, 0 newly installed, 2 to remove and 1 not upgraded.`
	result := parseAptSummary(output)
	assert.Contains(t, result, "Updated 12 packages")
	assert.Contains(t, result, "2 removed")
	assert.Contains(t, result, "1 held")
}

func TestParseAptSummary_UpgradesAndInstalls(t *testing.T) {
	output := `5 upgraded, 3 newly installed, 0 to remove and 0 not upgraded.`
	result := parseAptSummary(output)
	assert.Contains(t, result, "Updated 8 packages")
}

func TestParseDnfSummary_NothingToDo(t *testing.T) {
	output := `Last metadata expiration check: 0:12:34 ago.
Nothing to do.`
	result := parseDnfSummary(output)
	assert.Equal(t, "System is up to date", result)
}

func TestLastMeaningfulLines(t *testing.T) {
	output := "line1\nline2\n\nline3\n\n"
	result := lastMeaningfulLines(output, 2)
	assert.Equal(t, "line2\nline3", result)
}

func TestLastMeaningfulLines_LessLines(t *testing.T) {
	output := "only one"
	result := lastMeaningfulLines(output, 5)
	assert.Equal(t, "only one", result)
}

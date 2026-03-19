package collectors

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdatesCollectorName(t *testing.T) {
	c := &UpdatesCollector{}
	assert.Equal(t, "updates", c.Name())
}

func TestParseDNFCheckUpdate_WithUpdates(t *testing.T) {
	output := `kernel-core.x86_64                       6.7.5-200.fc39                updates
glibc.x86_64                              2.38-16.fc39                  updates
vim-enhanced.x86_64                       9.1.100-1.fc39                updates
`
	updates := parseDNFCheckUpdate(output)

	assert.Len(t, updates, 3)

	assert.Equal(t, "kernel-core", updates[0].Name)
	assert.Equal(t, "6.7.5-200.fc39", updates[0].NewVer)

	assert.Equal(t, "glibc", updates[1].Name)
	assert.Equal(t, "2.38-16.fc39", updates[1].NewVer)

	assert.Equal(t, "vim-enhanced", updates[2].Name)
	assert.Equal(t, "9.1.100-1.fc39", updates[2].NewVer)
}

func TestParseDNFCheckUpdate_Empty(t *testing.T) {
	updates := parseDNFCheckUpdate("")
	assert.Empty(t, updates)
}

func TestParseDNFCheckUpdate_BlankLines(t *testing.T) {
	output := `
kernel.x86_64                             6.7.5-200.fc39                updates

bash.x86_64                               5.2.26-1.fc39                 updates

`
	updates := parseDNFCheckUpdate(output)
	assert.Len(t, updates, 2)
	assert.Equal(t, "kernel", updates[0].Name)
	assert.Equal(t, "bash", updates[1].Name)
}

func TestParseDNFCheckUpdate_NoArchSuffix(t *testing.T) {
	// Edge case: package name without arch (unlikely but handle gracefully)
	output := `simplepackage                             1.0-1                         updates
`
	updates := parseDNFCheckUpdate(output)
	assert.Len(t, updates, 1)
	assert.Equal(t, "simplepackage", updates[0].Name)
	assert.Equal(t, "1.0-1", updates[0].NewVer)
}

func TestParseDNFCheckUpdate_NoarchPackages(t *testing.T) {
	output := `python3-docs.noarch                       3.12.2-1.fc39                 updates
tzdata.noarch                             2024a-1.fc39                  updates
`
	updates := parseDNFCheckUpdate(output)
	assert.Len(t, updates, 2)
	assert.Equal(t, "python3-docs", updates[0].Name)
	assert.Equal(t, "tzdata", updates[1].Name)
}

func TestParseDNFCheckUpdate_ShortLine(t *testing.T) {
	// Lines with fewer than 2 fields should be skipped
	output := `kernel-core.x86_64                        6.7.5-200.fc39                updates
badline
`
	updates := parseDNFCheckUpdate(output)
	assert.Len(t, updates, 1)
	assert.Equal(t, "kernel-core", updates[0].Name)
}

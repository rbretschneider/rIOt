package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashAPIKey(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		h1 := HashAPIKey("test-key-123")
		h2 := HashAPIKey("test-key-123")
		assert.Equal(t, h1, h2)
	})

	t.Run("different inputs produce different hashes", func(t *testing.T) {
		h1 := HashAPIKey("key-a")
		h2 := HashAPIKey("key-b")
		assert.NotEqual(t, h1, h2)
	})

	t.Run("non-empty output", func(t *testing.T) {
		h := HashAPIKey("any-key")
		require.NotEmpty(t, h)
		// SHA-256 hex digest is always 64 chars
		assert.Len(t, h, 64)
	})
}

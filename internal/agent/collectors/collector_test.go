package collectors

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubCollector is a minimal collector for testing the registry.
type stubCollector struct {
	name string
}

func (s *stubCollector) Name() string                                  { return s.name }
func (s *stubCollector) Collect(ctx context.Context) (interface{}, error) { return nil, nil }

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()
	c := &stubCollector{name: "test"}
	r.Register(c)

	assert.Len(t, r.Collectors(), 1)
	assert.Equal(t, "test", r.Collectors()[0].Name())
}

func TestRegistry_RegisterMultiple(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubCollector{name: "a"})
	r.Register(&stubCollector{name: "b"})
	r.Register(&stubCollector{name: "c"})

	collectors := r.Collectors()
	require.Len(t, collectors, 3)
	assert.Equal(t, "a", collectors[0].Name())
	assert.Equal(t, "b", collectors[1].Name())
	assert.Equal(t, "c", collectors[2].Name())
}

func TestRegistry_FilterEnabled_Subset(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubCollector{name: "cpu"})
	r.Register(&stubCollector{name: "memory"})
	r.Register(&stubCollector{name: "disk"})
	r.Register(&stubCollector{name: "network"})

	r.FilterEnabled([]string{"cpu", "disk"})

	collectors := r.Collectors()
	require.Len(t, collectors, 2)
	assert.Equal(t, "cpu", collectors[0].Name())
	assert.Equal(t, "disk", collectors[1].Name())
}

func TestRegistry_FilterEnabled_Empty(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubCollector{name: "cpu"})
	r.Register(&stubCollector{name: "memory"})

	r.FilterEnabled(nil) // empty filter keeps all

	assert.Len(t, r.Collectors(), 2)
}

func TestRegistry_FilterEnabled_Ordering(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubCollector{name: "a"})
	r.Register(&stubCollector{name: "b"})
	r.Register(&stubCollector{name: "c"})

	// Filter to b and a — order should follow registration, not filter order
	r.FilterEnabled([]string{"b", "a"})

	collectors := r.Collectors()
	require.Len(t, collectors, 2)
	assert.Equal(t, "a", collectors[0].Name(), "preserves registration order")
	assert.Equal(t, "b", collectors[1].Name())
}

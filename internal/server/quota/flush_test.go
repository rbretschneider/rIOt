package quota

import (
	"context"
	"errors"
	"testing"
	"time"
)

type failingFlushRepo struct {
	fail  bool
	saved map[string]Record
}

func (r *failingFlushRepo) Load(context.Context) ([]Record, error) { return nil, nil }
func (r *failingFlushRepo) Save(_ context.Context, v Record) error {
	if r.fail {
		return errors.New("test storage unavailable")
	}
	r.saved[v.DeviceID] = v
	return nil
}
func TestDUX_QuotaFlushRetriesAllUnsavedDevices(t *testing.T) {
	repo := &failingFlushRepo{fail: true, saved: map[string]Record{}}
	m := New(repo)
	now := time.Now()
	for _, id := range []string{"host-a", "host-b", "host-c"} {
		m.CommandOutput(id, "output", now)
	}
	if m.Flush(context.Background(), now) == nil {
		t.Fatal("expected failed flush")
	}
	for id, d := range m.devices {
		if !d.dirty {
			t.Fatalf("unsaved %s incorrectly marked clean", id)
		}
	}
	repo.fail = false
	if err := m.Flush(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(repo.saved) != 3 {
		t.Fatalf("lost unsaved quota states: %d", len(repo.saved))
	}
}

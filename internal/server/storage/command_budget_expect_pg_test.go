package storage

import (
	"context"
	"testing"
	"time"
)

// TestEX_SEC002_PG_BudgetSkipsPendingWindows pins EXPECT-001 C-2 (b): the
// command-results budget must never delete a command whose expected-activity
// window is not processed, because the delete would cascade its escalation
// markers and silently drop the backstop. Skips without a test database.
func TestEX_SEC002_PG_BudgetSkipsPendingWindows(t *testing.T) {
	g := commandBudgetPG(t)
	ctx := context.Background()
	now := time.Now()
	old := now.Add(-10 * 24 * time.Hour)
	_, err := g.pool.Exec(ctx, `INSERT INTO commands(id,status,created_at,updated_at,finished_at,result_bytes,action,window_closed_at) VALUES
		('pending-reboot','succeeded',$1,$1,$1,500,'reboot',NULL),
		('closed-reboot','succeeded',$1,$1,$1,500,'reboot',$1),
		('pending-other','succeeded',$1,$1,$1,500,'docker_start',NULL)`, old)
	if err != nil {
		t.Fatal(err)
	}
	before := now.Add(-7 * 24 * time.Hour)
	if _, err := g.purgeCommandResults(ctx, -1, &before); err != nil {
		t.Fatal(err)
	}
	rows, err := g.pool.Query(ctx, `SELECT id FROM commands ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var left []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		left = append(left, id)
	}
	if len(left) != 1 || left[0] != "pending-reboot" {
		t.Fatalf("[C-2 b] only the pending in-scope command may survive the budget purge, got %v", left)
	}
}

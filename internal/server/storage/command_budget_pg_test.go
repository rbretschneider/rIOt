package storage

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func commandBudgetPG(t *testing.T) *Guard {
	t.Helper()
	raw := os.Getenv("RIOT_TEST_PG_URL")
	u, e := url.Parse(raw)
	if raw == "" || e != nil || !strings.Contains(u.Path, "test") {
		t.Skip("disposable test database required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("dux_budget_%x", time.Now().UnixNano())
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(raw)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	_, e = pool.Exec(ctx, `CREATE TABLE commands(id text PRIMARY KEY,status text,created_at timestamptz,updated_at timestamptz,finished_at timestamptz,result_bytes bigint,action text NOT NULL DEFAULT '',window_closed_at timestamptz);CREATE TABLE command_output(id bigserial PRIMARY KEY,command_id text REFERENCES commands(id) ON DELETE CASCADE,created_at timestamptz,bytes bigint);CREATE TABLE docker_audit(id int);INSERT INTO docker_audit VALUES(1)`)
	if e != nil {
		t.Fatal(e)
	}
	return &Guard{pool: pool}
}
func seedCommandBudget(t *testing.T, g *Guard) {
	t.Helper()
	now := time.Now()
	_, err := g.pool.Exec(context.Background(), `INSERT INTO commands VALUES('old','succeeded',$1,$1,$1,500),('active','running',$1,$1,NULL,1000),('recent','succeeded',$2,$2,$2,400)`, now.Add(-10*24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.pool.Exec(context.Background(), `INSERT INTO command_output(command_id,created_at,bytes) VALUES('old',$1,500),('active',$1,500),('recent',$2,400)`, now.Add(-10*24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
}
func TestDUX_PG_EmergencyCommandResultsKeepRecentAndActive(t *testing.T) {
	g := commandBudgetPG(t)
	seedCommandBudget(t, g)
	ctx := context.Background()
	before := time.Now().Add(-7 * 24 * time.Hour)
	usage, e := g.purgeCommandResults(ctx, -1, &before)
	if e != nil {
		t.Fatal(e)
	}
	if usage != 2300 {
		t.Fatalf("unexpected retained bytes %d", usage)
	}
	var count int
	if e = g.pool.QueryRow(ctx, `SELECT count(*) FROM commands WHERE id IN('active','recent')`).Scan(&count); e != nil || count != 2 {
		t.Fatalf("recent/active commands lost: %d %v", count, e)
	}
	if e = g.pool.QueryRow(ctx, `SELECT count(*) FROM docker_audit`).Scan(&count); e != nil || count != 1 {
		t.Fatal("audit touched")
	}
}
func TestDUX_PG_FleetCommandBudgetDeletesChildrenFirst(t *testing.T) {
	g := commandBudgetPG(t)
	seedCommandBudget(t, g)
	ctx := context.Background()
	usage, e := g.purgeCommandResults(ctx, 1000, nil)
	if e != nil {
		t.Fatal(e)
	}
	if usage != 1500 {
		t.Fatalf("active command must remain even above budget, got %d", usage)
	}
	var count int
	if e = g.pool.QueryRow(ctx, `SELECT count(*) FROM commands WHERE status='running'`).Scan(&count); e != nil || count != 1 {
		t.Fatal("active command deleted")
	}
	if e = g.pool.QueryRow(ctx, `SELECT count(*) FROM command_output WHERE command_id<>'active'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("terminal output retained past budget")
	}
}

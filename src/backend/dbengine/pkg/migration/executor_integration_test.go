//go:build integration

package migration

import (
	"context"
	"testing"
	"time"

	"github.com/hakamavicena/if3110-dbengine/pkg/testpg"
)

func TestExecutorCommitsWholeMigration(t *testing.T) {
	pool := testpg.New(t)
	executor := &Executor{StatementTimeout: 5 * time.Second}
	if err := executor.Run(context.Background(), pool, `CREATE TABLE t(id int); INSERT INTO t VALUES (1);`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM t").Scan(&count); err != nil || count != 1 {
		t.Fatalf("row count = %d, err = %v", count, err)
	}
}

func TestExecutorRollsBackAllStatementsOnFailure(t *testing.T) {
	pool := testpg.New(t)
	executor := &Executor{StatementTimeout: 5 * time.Second}
	err := executor.Run(context.Background(), pool,
		`CREATE TABLE rollback_probe(id int); INSERT INTO missing_table VALUES (1);`)
	if err == nil {
		t.Fatal("expected statement error")
	}
	var exists bool
	if err := pool.QueryRow(context.Background(),
		"SELECT to_regclass('rollback_probe') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("first statement was not rolled back")
	}
}

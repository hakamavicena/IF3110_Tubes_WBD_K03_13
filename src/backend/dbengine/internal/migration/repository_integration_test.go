//go:build integration

package migration

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/hakamavicena/if3110-dbengine/internal/testpg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func integrationRepository(t *testing.T) (*PGRepository, *pgxpool.Pool) {
	t.Helper()
	pool := testpg.New(t)
	_, err := pool.Exec(context.Background(), `
		CREATE TABLE users (id BIGSERIAL PRIMARY KEY, username TEXT NOT NULL UNIQUE);
		CREATE TABLE database_targets (
			id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL UNIQUE, host TEXT NOT NULL,
			port INT NOT NULL DEFAULT 5432, database_name TEXT NOT NULL,
			username TEXT NOT NULL, password_enc BYTEA NOT NULL,
			ssl_mode TEXT NOT NULL DEFAULT 'prefer', created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE migrations (
			id BIGSERIAL PRIMARY KEY,
			database_id BIGINT NOT NULL REFERENCES database_targets(id) ON DELETE CASCADE,
			version INT NOT NULL, title TEXT NOT NULL, sql TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('draft','running','success','failed')),
			error_message TEXT, created_by BIGINT NOT NULL REFERENCES users(id),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			executed_by BIGINT REFERENCES users(id), executed_at TIMESTAMPTZ,
			duration_ms INT, UNIQUE (database_id, version)
		);
		INSERT INTO users (id, username) VALUES (1, 'tester');
		INSERT INTO database_targets (id, name, host, database_name, username, password_enc)
		VALUES (1, 'target', 'localhost', 'target', 'tester', decode('00','hex'));`, pgx.QueryExecModeSimpleProtocol)
	if err != nil {
		t.Fatal(err)
	}
	return NewPGRepository(pool), pool
}

func TestRepositoryCreateConcurrentVersionsAndLinearClaim(t *testing.T) {
	repo, _ := integrationRepository(t)
	ctx := context.Background()
	const count = 10
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := repo.Create(ctx, &Migration{
				DatabaseID: 1, Title: fmt.Sprintf("version %d", i), SQL: "SELECT 1", CreatedBy: 1,
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	items, err := repo.List(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != count {
		t.Fatalf("got %d versions, want %d", len(items), count)
	}
	for i, item := range items {
		if item.Version != count-i {
			t.Fatalf("version at position %d = %d", i, item.Version)
		}
	}

	versions := make(map[int]int64, len(items))
	for _, item := range items {
		versions[item.Version] = item.ID
	}
	if _, ok, err := repo.Claim(ctx, versions[2], 1); err != nil || ok {
		t.Fatalf("version 2 claim before version 1 success: ok=%v err=%v", ok, err)
	}
	if _, ok, err := repo.Claim(ctx, versions[1], 1); err != nil || !ok {
		t.Fatalf("version 1 claim: ok=%v err=%v", ok, err)
	}
	if _, ok, err := repo.Claim(ctx, versions[2], 1); err != nil || ok {
		t.Fatalf("version 2 claim while version 1 runs: ok=%v err=%v", ok, err)
	}
	if err := repo.Finish(ctx, versions[1], StatusSuccess, "", 1); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := repo.Claim(ctx, versions[2], 1); err != nil || !ok {
		t.Fatalf("version 2 claim after version 1 success: ok=%v err=%v", ok, err)
	}
}

func TestRepositoryOnlyOneConcurrentClaimAndRecoverStuck(t *testing.T) {
	repo, _ := integrationRepository(t)
	ctx := context.Background()
	item, err := repo.Create(ctx, &Migration{DatabaseID: 1, Title: "one", SQL: "SELECT 1", CreatedBy: 1})
	if err != nil {
		t.Fatal(err)
	}
	const count = 12
	var wg sync.WaitGroup
	results := make(chan bool, count)
	errors := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := repo.Claim(ctx, item.ID, 1)
			results <- ok
			errors <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	successes := 0
	for ok := range results {
		if ok {
			successes++
		}
	}
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful claims = %d, want 1", successes)
	}
	recovered, err := repo.RecoverStuck(ctx)
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v, want 1 recovery", recovered, err)
	}
	stored, err := repo.Get(ctx, item.ID)
	if err != nil || stored.Status != StatusFailed {
		t.Fatalf("status=%v err=%v, want failed", stored.Status, err)
	}
}

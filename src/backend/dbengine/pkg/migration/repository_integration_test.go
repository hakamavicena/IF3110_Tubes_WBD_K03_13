//go:build integration

package migration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"backend/dbengine/pkg/apperr"
	"backend/dbengine/pkg/testpg"

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
			duration_ms INT, 
			sql_down TEXT,
			is_irreversible BOOLEAN NOT NULL DEFAULT FALSE,
			irreversible_reason TEXT,
			is_immutable BOOLEAN NOT NULL DEFAULT FALSE,
			active_state TEXT NOT NULL DEFAULT 'not_applied',
			active_execution_id BIGINT,
			CONSTRAINT migrations_definition_reversible_check
				CHECK (
					(
						is_irreversible
						AND NULLIF(BTRIM(irreversible_reason), '') IS NOT NULL
					)
					OR
					(
						NOT is_irreversible
						AND NULLIF(BTRIM(sql_down), '') IS NOT NULL
					)
				),
			UNIQUE (database_id, version)
		);
		CREATE TABLE migration_history (
		execution_id BIGSERIAL PRIMARY KEY,
		database_id BIGINT NOT NULL REFERENCES database_targets(id),
		migration_id BIGINT NOT NULL REFERENCES migrations(id),
		job_id BIGINT,
		version INT NOT NULL,
		revision INT NOT NULL DEFAULT 1,
		title TEXT NOT NULL,
		action TEXT NOT NULL CHECK (action IN ('apply', 'rollback', 'forward')),
		sql_executed TEXT NOT NULL,
		executed_by BIGINT NOT NULL REFERENCES users(id),
		executed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		duration_ms INT NOT NULL DEFAULT 0,
		rows_affected BIGINT NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'success'
			CHECK (status IN ('success', 'failed')),
		reverted_execution_id BIGINT REFERENCES migration_history(execution_id));
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
				DatabaseID: 1,
				Title:      fmt.Sprintf("version %d", i),
				SQL:        "SELECT 1",
				SQLDown:    "SELECT 1",
				CreatedBy:  1,
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
		if item.SQLDown != "SELECT 1" {
			t.Errorf("migration %d SQLDown = %q, want %q", item.ID, item.SQLDown, "SELECT 1")
		}
		if item.IsIrreversible {
			t.Errorf("migration %d unexpectedly marked irreversible", item.ID)
		}

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
	repo, pool := integrationRepository(t)
	ctx := context.Background()
	item, err := repo.Create(ctx, &Migration{
		DatabaseID: 1,
		Title:      "one",
		SQL:        "SELECT 1",
		SQLDown:    "SELECT 1",
		CreatedBy:  1})
	if err != nil {
		t.Fatal(err)
	}
	const count = 12
	var wg sync.WaitGroup
	results := make(chan bool, count)
	claimErrors := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := repo.Claim(ctx, item.ID, 1)
			results <- ok
			claimErrors <- err
		}()
	}
	wg.Wait()
	close(results)
	close(claimErrors)
	successes := 0
	for ok := range results {
		if ok {
			successes++
		}
	}
	for err := range claimErrors {
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
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusFailed {
		t.Fatalf("status=%v, want failed", stored.Status)
	}

	var immutable bool
	if err := pool.QueryRow(
		ctx,
		"SELECT is_immutable FROM migrations WHERE id=$1",
		item.ID,
	).Scan(&immutable); err != nil {
		t.Fatal(err)
	}
	if !immutable {
		t.Fatal("migration should be immutable after execution was claimed")
	}

	if err := repo.UpdateDraft(ctx, item.ID, 1, "edited", "SELECT 2", "SELECT 1", false, ""); !errors.Is(err, apperr.ErrNotExecutable) {
		t.Fatalf("editing an executed migration error = %v, want ErrNotExecutable", err)
	}
}

func TestRepositoryClaimRollbackOnlyLatestActiveMigration(t *testing.T) {
	repo, pool := integrationRepository(t)
	ctx := context.Background()

	items := make([]*Migration, 3)
	for i := range items {
		version := i + 1
		item, err := repo.Create(ctx, &Migration{
			DatabaseID: 1,
			Title:      fmt.Sprintf("migration %d", version),
			SQL:        fmt.Sprintf("SELECT %d", version),
			SQLDown:    fmt.Sprintf("SELECT %d", version+10),
			CreatedBy:  1,
		})
		if err != nil {
			t.Fatal(err)
		}
		items[i] = item

		executionID := int64(100 + version)
		_, err = pool.Exec(ctx, `
            UPDATE migrations
            SET status = 'success',
                active_state = 'applied',
                active_execution_id = $2
            WHERE id = $1`,
			item.ID, executionID,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	if _, _, ok, err := repo.ClaimRollback(ctx, items[1].ID, 1); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("older migration was claimable while a newer migration was active")
	}

	sqlDown, executionID, ok, err := repo.ClaimRollback(ctx, items[2].ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("latest active migration was not claimable")
	}
	if sqlDown != "SELECT 13" {
		t.Errorf("rollback SQL = %q, want %q", sqlDown, "SELECT 13")
	}
	if executionID != 103 {
		t.Errorf("reverted execution ID = %d, want 103", executionID)
	}

	if _, _, ok, err := repo.ClaimRollback(ctx, items[2].ID, 1); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("migration was claimable a second time after being claimed")
	}
}

func TestRepositoryClaimRollbackRejectsIrreversibleMigration(t *testing.T) {
	repo, pool := integrationRepository(t)
	ctx := context.Background()

	item, err := repo.Create(ctx, &Migration{
		DatabaseID:         1,
		Title:              "irreversible migration",
		SQL:                "DELETE FROM items",
		IsIrreversible:     true,
		IrreversibleReason: "Deleted data cannot be recovered.",
		CreatedBy:          1,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `
        UPDATE migrations
        SET status = 'success',
            active_state = 'applied',
            active_execution_id = 201
        WHERE id = $1`,
		item.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, ok, err := repo.ClaimRollback(ctx, item.ID, 1); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("irreversible migration was claimable for rollback")
	}
}

func TestRepositoryRecordExecutionApplyRollbackForward(t *testing.T) {
	repo, pool := integrationRepository(t)
	ctx := context.Background()

	item, err := repo.Create(ctx, &Migration{
		DatabaseID: 1,
		Title:      "create items",
		SQL:        "CREATE TABLE items (id INT)",
		SQLDown:    "DROP TABLE items",
		CreatedBy:  1,
	})
	if err != nil {
		t.Fatal(err)
	}

	applied, err := repo.RecordExecution(ctx, &Execution{
		DatabaseID:  1,
		MigrationID: item.ID,
		Version:     item.Version,
		Revision:    1,
		Title:       item.Title,
		Action:      ActionApply,
		SQLExecuted: item.SQL,
		ExecutedBy:  1,
		Status:      StatusSuccess,
	})
	if err != nil {
		t.Fatalf("record apply execution: %v", err)
	}

	rolledBack, err := repo.RecordExecution(ctx, &Execution{
		DatabaseID:          1,
		MigrationID:         item.ID,
		Version:             item.Version,
		Revision:            1,
		Title:               item.Title,
		Action:              ActionRollback,
		SQLExecuted:         item.SQLDown,
		ExecutedBy:          1,
		Status:              StatusSuccess,
		RevertedExecutionID: &applied.ExecutionID,
	})
	if err != nil {
		t.Fatalf("record rollback execution: %v", err)
	}

	forwarded, err := repo.RecordExecution(ctx, &Execution{
		DatabaseID:          1,
		MigrationID:         item.ID,
		Version:             item.Version,
		Revision:            1,
		Title:               item.Title,
		Action:              ActionForward,
		SQLExecuted:         item.SQL,
		ExecutedBy:          1,
		Status:              StatusSuccess,
		RevertedExecutionID: &rolledBack.ExecutionID,
	})
	if err != nil {
		t.Fatalf("record forward execution: %v", err)
	}

	var activeState string
	var activeExecutionID int64
	err = pool.QueryRow(ctx, `
        SELECT active_state, active_execution_id
        FROM migrations
        WHERE id = $1`,
		item.ID,
	).Scan(&activeState, &activeExecutionID)
	if err != nil {
		t.Fatal(err)
	}

	if activeState != "applied" {
		t.Errorf("active_state = %q, want %q", activeState, "applied")
	}
	if activeExecutionID != forwarded.ExecutionID {
		t.Errorf("active_execution_id = %d, want %d",
			activeExecutionID, forwarded.ExecutionID)
	}
}

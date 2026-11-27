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
		CREATE OR REPLACE FUNCTION prevent_history_modification()
		RETURNS TRIGGER AS $$
		BEGIN
			RAISE EXCEPTION 'migration_history is immutable';
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER trg_immutable_migration_history
		BEFORE UPDATE OR DELETE ON migration_history
		FOR EACH ROW EXECUTE FUNCTION prevent_history_modification();
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

func TestRepositoryRollbackAndForwardFollowLinearOrder(t *testing.T) {
	repo, pool := integrationRepository(t)
	ctx := context.Background()

	migrations := make([]*Migration, 3)
	applied := make([]*Execution, 3)
	rolledBack := make([]*Execution, 3)
	forwarded := make([]*Execution, 3)

	for i := range migrations {
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
		migrations[i] = item
	}

	recordSuccess := func(
		item *Migration,
		action ExecutionAction,
		sql string,
		revertedExecutionID *int64,
	) *Execution {
		t.Helper()

		execution, err := repo.RecordExecution(ctx, &Execution{
			DatabaseID:          1,
			MigrationID:         item.ID,
			Version:             item.Version,
			Revision:            1,
			Title:               item.Title,
			Action:              action,
			SQLExecuted:         sql,
			ExecutedBy:          1,
			Status:              StatusSuccess,
			RevertedExecutionID: revertedExecutionID,
		})
		if err != nil {
			t.Fatalf("record %s execution for version %d: %v", action, item.Version, err)
		}
		return execution
	}

	// Apply A, B, C in order so all three are active.
	for i, item := range migrations {
		sqlUp, claimed, err := repo.Claim(ctx, item.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !claimed {
			t.Fatalf("version %d was not claimable for apply", item.Version)
		}
		if sqlUp != item.SQL {
			t.Fatalf("apply SQL = %q, want %q", sqlUp, item.SQL)
		}

		if err := repo.Finish(ctx, item.ID, StatusSuccess, "", 1); err != nil {
			t.Fatal(err)
		}
		applied[i] = recordSuccess(item, ActionApply, sqlUp, nil)
	}

	// B must not be rollback-claimable while C is still active.
	if _, _, claimed, err := repo.ClaimRollback(ctx, migrations[1].ID, 1); err != nil {
		t.Fatal(err)
	} else if claimed {
		t.Fatal("version B was claimable while version C was active")
	}

	// Roll back C, then B, then A.
	for i := len(migrations) - 1; i >= 0; i-- {
		item := migrations[i]
		sqlDown, revertedID, claimed, err := repo.ClaimRollback(ctx, item.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !claimed {
			t.Fatalf("version %d was not claimable for rollback", item.Version)
		}
		if sqlDown != item.SQLDown {
			t.Errorf("rollback SQL for version %d = %q, want %q",
				item.Version, sqlDown, item.SQLDown)
		}
		if revertedID != applied[i].ExecutionID {
			t.Errorf("rollback of version %d references execution %d, want %d",
				item.Version, revertedID, applied[i].ExecutionID)
		}

		if err := repo.Finish(ctx, item.ID, StatusSuccess, "", 1); err != nil {
			t.Fatal(err)
		}
		revertedExecutionID := revertedID
		rolledBack[i] = recordSuccess(
			item,
			ActionRollback,
			sqlDown,
			&revertedExecutionID,
		)
	}

	// C must not be forward-claimable while A and B remain rolled back.
	if _, _, claimed, err := repo.ClaimForward(ctx, migrations[2].ID, 1); err != nil {
		t.Fatal(err)
	} else if claimed {
		t.Fatal("version C was claimable before versions A and B were forwarded")
	}

	// Forward A, then B, then C.
	for i, item := range migrations {
		sqlUp, rollbackExecutionID, claimed, err := repo.ClaimForward(ctx, item.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !claimed {
			t.Fatalf("version %d was not claimable for forward", item.Version)
		}
		if sqlUp != item.SQL {
			t.Errorf("forward SQL for version %d = %q, want %q",
				item.Version, sqlUp, item.SQL)
		}
		if rollbackExecutionID != rolledBack[i].ExecutionID {
			t.Errorf("forward of version %d references rollback execution %d, want %d",
				item.Version, rollbackExecutionID, rolledBack[i].ExecutionID)
		}

		if err := repo.Finish(ctx, item.ID, StatusSuccess, "", 1); err != nil {
			t.Fatal(err)
		}
		revertedExecutionID := rollbackExecutionID
		forwarded[i] = recordSuccess(
			item,
			ActionForward,
			sqlUp,
			&revertedExecutionID,
		)
	}

	// Check final active state and history order.
	for i, item := range migrations {
		var activeState string
		var activeExecutionID int64
		if err := pool.QueryRow(ctx, `
            SELECT active_state, active_execution_id
            FROM migrations
            WHERE id = $1`,
			item.ID,
		).Scan(&activeState, &activeExecutionID); err != nil {
			t.Fatal(err)
		}

		if activeState != "applied" {
			t.Errorf("version %d active_state = %q, want applied",
				item.Version, activeState)
		}
		if activeExecutionID != forwarded[i].ExecutionID {
			t.Errorf("version %d active_execution_id = %d, want %d",
				item.Version, activeExecutionID, forwarded[i].ExecutionID)
		}
	}

	history, err := repo.ListHistory(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 9 {
		t.Fatalf("history entries = %d, want 9", len(history))
	}

	wantActions := []ExecutionAction{
		ActionApply, ActionApply, ActionApply,
		ActionRollback, ActionRollback, ActionRollback,
		ActionForward, ActionForward, ActionForward,
	}
	wantMigrationIDs := []int64{
		migrations[0].ID, migrations[1].ID, migrations[2].ID,
		migrations[2].ID, migrations[1].ID, migrations[0].ID,
		migrations[0].ID, migrations[1].ID, migrations[2].ID,
	}
	for i, execution := range history {
		if execution.Action != wantActions[i] {
			t.Errorf("history[%d].Action = %q, want %q",
				i, execution.Action, wantActions[i])
		}
		if execution.MigrationID != wantMigrationIDs[i] {
			t.Errorf("history[%d].MigrationID = %d, want %d",
				i, execution.MigrationID, wantMigrationIDs[i])
		}
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

func TestRepositoryListsFailedAttemptsAndKeepsHistoryImmutable(t *testing.T) {
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
		t.Fatalf("record successful execution: %v", err)
	}

	failed, err := repo.RecordExecution(ctx, &Execution{
		DatabaseID:          1,
		MigrationID:         item.ID,
		Version:             item.Version,
		Revision:            1,
		Title:               item.Title,
		Action:              ActionRollback,
		SQLExecuted:         item.SQLDown,
		ExecutedBy:          1,
		Status:              StatusFailed,
		RevertedExecutionID: &applied.ExecutionID,
	})
	if err != nil {
		t.Fatalf("record failed execution attempt: %v", err)
	}

	history, err := repo.ListHistory(ctx, 1)
	if err != nil {
		t.Fatalf("list migration history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history has %d entries, want 2", len(history))
	}
	if history[0].ExecutionID != applied.ExecutionID ||
		history[1].ExecutionID != failed.ExecutionID {
		t.Fatalf("history order = [%d, %d], want [%d, %d]",
			history[0].ExecutionID, history[1].ExecutionID,
			applied.ExecutionID, failed.ExecutionID)
	}
	if history[1].Status != StatusFailed ||
		history[1].SQLExecuted != item.SQLDown ||
		history[1].ExecutedByName != "tester" ||
		history[1].RevertedExecutionID == nil ||
		*history[1].RevertedExecutionID != applied.ExecutionID {
		t.Fatalf("failed history attempt has incorrect snapshot or metadata: %+v", history[1])
	}

	for _, query := range []string{
		"UPDATE migration_history SET title='changed' WHERE execution_id=$1",
		"DELETE FROM migration_history WHERE execution_id=$1",
	} {
		if _, err := pool.Exec(ctx, query, applied.ExecutionID); err == nil {
			t.Errorf("history mutation unexpectedly succeeded: %s", query)
		}
	}
}

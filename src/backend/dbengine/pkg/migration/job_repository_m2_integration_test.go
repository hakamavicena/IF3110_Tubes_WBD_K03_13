//go:build integration

package migration

import (
	"context"
	"testing"

	"backend/dbengine/pkg/testpg"
	"github.com/jackc/pgx/v5"
)

func TestUpdateDraftWithJobClearsPreviousApprovals(t *testing.T) {
	pool := testpg.New(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		CREATE TABLE users (
			id BIGINT PRIMARY KEY,
			username TEXT NOT NULL
		);
		CREATE TABLE database_targets (
			id BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
			port INT NOT NULL DEFAULT 5432,
			database_name TEXT NOT NULL,
			username TEXT NOT NULL,
			password_enc BYTEA NOT NULL,
			ssl_mode TEXT NOT NULL DEFAULT 'prefer',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE migrations (
			id BIGINT PRIMARY KEY,
			database_id BIGINT NOT NULL REFERENCES database_targets(id),
			version INT NOT NULL,
			revision INT NOT NULL DEFAULT 1,
			title TEXT NOT NULL,
			sql TEXT NOT NULL,
			sql_down TEXT,
			is_irreversible BOOLEAN NOT NULL DEFAULT FALSE,
			irreversible_reason TEXT,
			execution_mode TEXT NOT NULL DEFAULT 'standard',
			batch_column TEXT,
			batch_size INT,
			is_immutable BOOLEAN NOT NULL DEFAULT FALSE,
			active_state TEXT NOT NULL DEFAULT 'not_applied',
			active_execution_id BIGINT,
			status TEXT NOT NULL,
			error_message TEXT,
			created_by BIGINT NOT NULL REFERENCES users(id),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			executed_by BIGINT,
			executed_at TIMESTAMPTZ,
			duration_ms INT
		);
		CREATE TABLE migration_jobs (
			id BIGINT PRIMARY KEY,
			database_id BIGINT NOT NULL REFERENCES database_targets(id),
			migration_id BIGINT NOT NULL REFERENCES migrations(id),
			action TEXT NOT NULL,
			status TEXT NOT NULL,
			reverts_execution_id BIGINT,
			sql_snapshot TEXT NOT NULL,
			execution_mode TEXT NOT NULL DEFAULT 'standard',
			batch_column TEXT,
			batch_size INT,
			committed_batches INT NOT NULL DEFAULT 0,
			committed_rows BIGINT NOT NULL DEFAULT 0,
			last_cursor TEXT,
			retry_count INT NOT NULL DEFAULT 0,
			error_message TEXT,
			trace_parent TEXT,
			requested_by BIGINT NOT NULL REFERENCES users(id),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			queued_at TIMESTAMPTZ,
			started_at TIMESTAMPTZ,
			finished_at TIMESTAMPTZ,
			duration_ms INT
		);
		CREATE TABLE migration_approvals (
			job_id BIGINT NOT NULL REFERENCES migration_jobs(id),
			group_id BIGINT NOT NULL,
			leader_id BIGINT NOT NULL,
			approved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (job_id, group_id)
		);
		INSERT INTO users (id, username) VALUES (1, 'editor');
		INSERT INTO database_targets (id, name, host, database_name, username, password_enc)
		VALUES (1, 'target', 'localhost', 'target', 'editor', decode('00', 'hex'));
		INSERT INTO migrations (
			id, database_id, version, title, sql, sql_down, status, error_message, created_by
		) VALUES (
			1, 1, 1, 'failed migration', 'UPDATE items SET value = 1',
			'UPDATE items SET value = 0', 'failed', 'constraint violation', 1
		);
		INSERT INTO migration_jobs (
			id, database_id, migration_id, action, status, sql_snapshot, requested_by
		) VALUES (
			1, 1, 1, 'apply', 'failed', 'UPDATE items SET value = 1', 1
		);
		INSERT INTO migration_approvals (job_id, group_id, leader_id)
		VALUES (1, 10, 2), (1, 20, 3);`,
		pgx.QueryExecModeSimpleProtocol)
	if err != nil {
		t.Fatal(err)
	}

	repo := NewPGRepository(pool)
	updated, job, err := repo.UpdateDraftWithJob(ctx, 1, 1, 1, MigrationInputM2{
		Title:         "corrected migration",
		SQL:           "UPDATE items SET value = 2",
		SQLDown:       "UPDATE items SET value = 0",
		ExecutionMode: ExecutionModeStandard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusDraft || job.Status != StatusDraft {
		t.Fatalf("migration/job status = %q/%q, want draft/draft", updated.Status, job.Status)
	}
	if job.SQLSnapshot != "UPDATE items SET value = 2" {
		t.Fatalf("job SQL snapshot = %q, want corrected SQL", job.SQLSnapshot)
	}

	var approvalCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM migration_approvals WHERE job_id = $1`, job.ID).Scan(&approvalCount); err != nil {
		t.Fatal(err)
	}
	if approvalCount != 0 {
		t.Fatalf("stale approvals = %d, want 0 after editing failed migration", approvalCount)
	}
}

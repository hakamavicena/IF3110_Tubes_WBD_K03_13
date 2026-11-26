package migration

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"backend/dbengine/pkg/apperr"
	"github.com/jackc/pgx/v5"
)

const selectJobColumns = `
	SELECT id, database_id, migration_id, action, status, reverts_execution_id,
	       sql_snapshot, execution_mode, COALESCE(batch_column, ''), COALESCE(batch_size, 0),
	       committed_batches, committed_rows, COALESCE(last_cursor, ''), retry_count,
	       COALESCE(error_message, ''), COALESCE(trace_parent, ''), requested_by,
	       created_at, queued_at, started_at, finished_at, duration_ms
	FROM migration_jobs`

func scanJob(row pgx.Row) (*Job, error) {
	var j Job
	var action, status, execMode string
	err := row.Scan(
		&j.ID, &j.DatabaseID, &j.MigrationID, &action, &status, &j.RevertsExecutionID,
		&j.SQLSnapshot, &execMode, &j.BatchColumn, &j.BatchSize,
		&j.CommittedBatches, &j.CommittedRows, &j.LastCursor, &j.RetryCount,
		&j.ErrorMessage, &j.TraceParent, &j.RequestedBy,
		&j.CreatedAt, &j.QueuedAt, &j.StartedAt, &j.FinishedAt, &j.DurationMs,
	)
	if err != nil {
		return nil, err
	}
	j.Action = JobAction(action)
	j.Status = Status(status)
	j.ExecutionMode = ExecutionMode(execMode)
	return &j, nil
}

func (r *PGRepository) GetJob(ctx context.Context, databaseID, jobID int64) (*Job, error) {
	j, err := scanJob(r.db.QueryRow(ctx, selectJobColumns+" WHERE id = $1 AND database_id = $2", jobID, databaseID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	return j, err
}

func (r *PGRepository) ListJobsByMigration(ctx context.Context, databaseID, migrationID int64) ([]Job, error) {
	rows, err := r.db.Query(ctx, selectJobColumns+" WHERE database_id = $1 AND migration_id = $2 ORDER BY id DESC", databaseID, migrationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (r *PGRepository) MarkJobQueued(ctx context.Context, databaseID, jobID int64, traceParent string) (*Job, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE migration_jobs
		SET status = 'pending_queue',
		    trace_parent = COALESCE(NULLIF($3, ''), trace_parent),
		    queued_at = now(),
		    error_message = NULL
		WHERE id = $1 AND database_id = $2 AND status = 'approved'`,
		jobID, databaseID, traceParent,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("%w: job harus berstatus 'approved' sebelum dimasukkan ke antrean", apperr.ErrNotExecutable)
	}
	_, _ = r.db.Exec(ctx, `
		UPDATE migrations
		SET status = 'pending_queue'
		WHERE id = (SELECT migration_id FROM migration_jobs WHERE id = $1)`,
		jobID,
	)
	return r.GetJob(ctx, databaseID, jobID)
}

func (r *PGRepository) MarkJobRunning(ctx context.Context, databaseID, jobID int64) (*Job, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE migration_jobs
		SET status = 'running',
		    started_at = COALESCE(started_at, now()),
		    error_message = NULL
		WHERE id = $1 AND database_id = $2 AND status IN ('pending_queue', 'approved', 'running')`,
		jobID, databaseID,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("%w: job %d tidak dalam status siap dijalankan", apperr.ErrNotExecutable, jobID)
	}
	_, _ = r.db.Exec(ctx, `
		UPDATE migrations
		SET status = 'running'
		WHERE id = (SELECT migration_id FROM migration_jobs WHERE id = $1)`,
		jobID,
	)
	return r.GetJob(ctx, databaseID, jobID)
}

func (r *PGRepository) UpdateJobBatchProgress(ctx context.Context, jobID int64, progress BatchProgress) error {
	_, err := r.db.Exec(ctx, `
		UPDATE migration_jobs
		SET committed_batches = $2,
		    committed_rows = $3,
		    last_cursor = NULLIF($4, '')
		WHERE id = $1`,
		jobID, progress.CommittedBatches, progress.CommittedRows, progress.LastCursor,
	)
	return err
}

func (r *PGRepository) ResolveCheckpointJobID(ctx context.Context, databaseID, migrationID, currentJobID int64) (int64, error) {
	var originJobID int64
	err := r.db.QueryRow(ctx, `
		SELECT id
		FROM migration_jobs
		WHERE database_id = $1
		  AND migration_id = $2
		  AND action IN ('apply', 'forward')
		  AND execution_mode = 'batch'
		ORDER BY id DESC
		LIMIT 1`,
		databaseID, migrationID,
	).Scan(&originJobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return currentJobID, nil
	}
	if err != nil {
		return 0, err
	}
	return originJobID, nil
}

func (r *PGRepository) CreateBatchRecoveryJob(
	ctx context.Context,
	databaseID, migrationID, userID int64,
	action JobAction,
) (*Job, error) {
	if action != JobActionRetryBatch && action != JobActionCancelBatch {
		return nil, fmt.Errorf("%w: aksi pemulihan batch harus retry_batch atau cancel_batch", apperr.ErrInvalidInput)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var migStatus, activeState, execMode, sqlUp, sqlDown, batchCol string
	var isIrreversible bool
	var batchSize int
	err = tx.QueryRow(ctx, `
		SELECT status, active_state, execution_mode, sql, COALESCE(sql_down, ''),
		       is_irreversible, COALESCE(batch_column, ''), COALESCE(batch_size, 0)
		FROM migrations
		WHERE id = $1 AND database_id = $2
		FOR UPDATE`,
		migrationID, databaseID,
	).Scan(&migStatus, &activeState, &execMode, &sqlUp, &sqlDown, &isIrreversible, &batchCol, &batchSize)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if ExecutionMode(execMode) != ExecutionModeBatch {
		return nil, fmt.Errorf("%w: migration ini bukan mode batch", apperr.ErrInvalidInput)
	}

	isRecoverable := Status(migStatus) == StatusPartial || Status(migStatus) == StatusPartiallyReverted ||
		ActiveState(activeState) == ActiveStatePartial || ActiveState(activeState) == ActiveStatePartiallyReverted

	if !isRecoverable {
		return nil, fmt.Errorf("%w: pemulihan batch hanya dapat diajukan saat status migration partial atau partially_reverted", apperr.ErrNotExecutable)
	}

	sqlSnapshot := sqlUp
	if action == JobActionCancelBatch {
		if isIrreversible || strings.TrimSpace(sqlDown) == "" {
			return nil, fmt.Errorf("%w: batch migration yang irreversible atau tanpa sql_down tidak dapat di-cancel", apperr.ErrInvalidInput)
		}
		sqlSnapshot = sqlDown
	}

	var prevBatches, prevRetries int
	var prevRows int64
	var prevCursor string
	_ = tx.QueryRow(ctx, `
		SELECT committed_batches, committed_rows, COALESCE(last_cursor, ''), retry_count
		FROM migration_jobs
		WHERE database_id = $1 AND migration_id = $2
		ORDER BY id DESC
		LIMIT 1`,
		databaseID, migrationID,
	).Scan(&prevBatches, &prevRows, &prevCursor, &prevRetries)

	var newJobID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO migration_jobs (
			database_id, migration_id, action, status, sql_snapshot,
			execution_mode, batch_column, batch_size,
			committed_batches, committed_rows, last_cursor, retry_count, requested_by
		) VALUES (
			$1, $2, $3, 'draft', $4,
			'batch', $5, $6,
			$7, $8, NULLIF($9, ''), $10, $11
		) RETURNING id`,
		databaseID, migrationID, string(action), sqlSnapshot,
		batchCol, batchSize,
		prevBatches, prevRows, prevCursor, prevRetries+1, userID,
	).Scan(&newJobID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetJob(ctx, databaseID, newJobID)
}

func (r *PGRepository) FinalizeJobExecution(
	ctx context.Context,
	job *Job,
	finalStatus Status,
	progress BatchProgress,
	durationMs int,
	errMsg string,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE migration_jobs
		SET status = $2,
		    committed_batches = $3,
		    committed_rows = $4,
		    last_cursor = NULLIF($5, ''),
		    error_message = NULLIF($6, ''),
		    finished_at = now(),
		    duration_ms = $7
		WHERE id = $1`,
		job.ID, string(finalStatus),
		progress.CommittedBatches, progress.CommittedRows, progress.LastCursor,
		errMsg, durationMs,
	)
	if err != nil {
		return err
	}

	var migVersion, migRevision int
	var migTitle string
	var currentActiveState string
	var currentActiveExecID *int64
	err = tx.QueryRow(ctx, `
		SELECT version, revision, title, active_state, active_execution_id
		FROM migrations
		WHERE id = $1
		FOR UPDATE`,
		job.MigrationID,
	).Scan(&migVersion, &migRevision, &migTitle, &currentActiveState, &currentActiveExecID)
	if err != nil {
		return err
	}

	newActiveState := currentActiveState
	isImmutable := false
	switch finalStatus {
	case StatusSuccess:
		isImmutable = true
		switch job.Action {
		case JobActionApply, JobActionForward, JobActionRetryBatch:
			newActiveState = string(ActiveStateApplied)
		case JobActionRollback:
			newActiveState = string(ActiveStateRolledBack)
		case JobActionCancelBatch:
			newActiveState = string(ActiveStateNotApplied)
		}
	case StatusPartial:
		isImmutable = true
		newActiveState = string(ActiveStatePartial)
	case StatusPartiallyReverted:
		isImmutable = true
		newActiveState = string(ActiveStatePartiallyReverted)
	}

	shouldRecordHistory := finalStatus == StatusSuccess && job.Action != JobActionCancelBatch

	var newExecutionID *int64
	if shouldRecordHistory {
		historyAction := string(job.Action)
		if job.Action == JobActionRetryBatch {
			historyAction = string(JobActionApply)
		}
		var execID int64
		err = tx.QueryRow(ctx, `
			INSERT INTO migration_history (
				database_id, migration_id, job_id, version, revision, title,
				action, sql_executed, executed_by, executed_at, duration_ms,
				rows_affected, status, reverted_execution_id
			) VALUES (
				$1, $2, $3, $4, $5, $6,
				$7, $8, $9, now(), $10,
				$11, 'success', $12
			) RETURNING execution_id`,
			job.DatabaseID, job.MigrationID, job.ID, migVersion, migRevision, migTitle,
			historyAction, job.SQLSnapshot, job.RequestedBy, durationMs,
			progress.CommittedRows, job.RevertsExecutionID,
		).Scan(&execID)
		if err != nil {
			return err
		}
		newExecutionID = &execID
	} else {
		newExecutionID = currentActiveExecID
	}

	_, err = tx.Exec(ctx, `
		UPDATE migrations
		SET status = $2,
		    active_state = $3,
		    active_execution_id = $4,
		    is_immutable = is_immutable OR $5,
		    error_message = NULLIF($6, ''),
		    executed_by = $7,
		    executed_at = now(),
		    duration_ms = $8
		WHERE id = $1`,
		job.MigrationID, string(finalStatus), newActiveState, newExecutionID,
		isImmutable, errMsg, job.RequestedBy, durationMs,
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

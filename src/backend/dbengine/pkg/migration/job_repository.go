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

type MigrationInputM2 struct {
	Title              string
	SQL                string
	SQLDown            string
	IsIrreversible     bool
	IrreversibleReason string
	ExecutionMode      ExecutionMode
	BatchColumn        string
	BatchSize          int
}

func ValidateMigrationInputM2(in MigrationInputM2) error {
	if err := ValidateInput(in.Title, in.SQL); err != nil {
		return err
	}
	if in.IsIrreversible {
		if strings.TrimSpace(in.IrreversibleReason) == "" {
			return fmt.Errorf("%w: alasan irreversible wajib diisi jika migration ditandai irreversible", apperr.ErrInvalidInput)
		}
	} else {
		if strings.TrimSpace(in.SQLDown) == "" {
			return fmt.Errorf("%w: sql_down wajib diisi untuk migration yang bukan irreversible", apperr.ErrInvalidInput)
		}
		if err := ValidateInput(in.Title, in.SQLDown); err != nil {
			return err
		}
	}
	mode := in.ExecutionMode
	if mode == "" {
		mode = ExecutionModeStandard
	}
	if mode != ExecutionModeStandard && mode != ExecutionModeBatch {
		return fmt.Errorf("%w: execution_mode harus 'standard' atau 'batch'", apperr.ErrInvalidInput)
	}
	if mode == ExecutionModeBatch {
		if !reIdentPart.MatchString(strings.TrimSpace(in.BatchColumn)) {
			return fmt.Errorf("%w: batch_column wajib diisi dengan nama kolom yang valid pada mode batch", apperr.ErrInvalidInput)
		}
		if in.BatchSize <= 0 {
			return fmt.Errorf("%w: batch_size harus lebih besar dari 0 pada mode batch", apperr.ErrInvalidInput)
		}
		if _, err := extractTargetTable(in.SQL); err != nil {
			return err
		}
	}
	return nil
}

const selectMigrationM2 = `
	SELECT m.id, m.database_id, m.version, m.revision, m.title, m.sql,
	       COALESCE(m.sql_down, ''), m.is_irreversible, COALESCE(m.irreversible_reason, ''),
	       m.execution_mode, COALESCE(m.batch_column, ''), COALESCE(m.batch_size, 0),
	       m.is_immutable, m.active_state, m.active_execution_id, m.status,
	       COALESCE(m.error_message, ''), m.created_by, cu.username, m.created_at,
	       m.executed_by, eu.username, m.executed_at, m.duration_ms
	FROM migrations m
	JOIN users cu ON cu.id = m.created_by
	LEFT JOIN users eu ON eu.id = m.executed_by`

func scanMigrationM2(row pgx.Row) (*Migration, error) {
	var m Migration
	var execMode, activeState, status string
	err := row.Scan(
		&m.ID, &m.DatabaseID, &m.Version, &m.Revision, &m.Title, &m.SQL,
		&m.SQLDown, &m.IsIrreversible, &m.IrreversibleReason,
		&execMode, &m.BatchColumn, &m.BatchSize,
		&m.IsImmutable, &activeState, &m.ActiveExecutionID, &status,
		&m.ErrorMessage, &m.CreatedBy, &m.CreatedByName, &m.CreatedAt,
		&m.ExecutedBy, &m.ExecutedByName, &m.ExecutedAt, &m.DurationMs,
	)
	if err != nil {
		return nil, err
	}
	m.ExecutionMode = ExecutionMode(execMode)
	m.ActiveState = ActiveState(activeState)
	m.Status = Status(status)
	return &m, nil
}

func (r *PGRepository) GetM2(ctx context.Context, id int64) (*Migration, error) {
	m, err := scanMigrationM2(r.db.QueryRow(ctx, selectMigrationM2+" WHERE m.id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	return m, err
}

func (r *PGRepository) ListM2(ctx context.Context, databaseID int64) ([]Migration, error) {
	rows, err := r.db.Query(ctx, selectMigrationM2+" WHERE m.database_id = $1 ORDER BY m.version DESC", databaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Migration
	for rows.Next() {
		m, err := scanMigrationM2(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *PGRepository) CreateMigrationWithJob(ctx context.Context, databaseID, userID int64, in MigrationInputM2) (*Migration, *Job, error) {
	if err := ValidateMigrationInputM2(in); err != nil {
		return nil, nil, err
	}
	mode := in.ExecutionMode
	if mode == "" {
		mode = ExecutionModeStandard
	}
	batchCol := strings.TrimSpace(in.BatchColumn)
	var batchSizePtr *int
	if mode == ExecutionModeBatch && in.BatchSize > 0 {
		sz := in.BatchSize
		batchSizePtr = &sz
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", databaseID); err != nil {
		return nil, nil, err
	}

	var migID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO migrations (
			database_id, version, revision, title, sql, sql_down,
			is_irreversible, irreversible_reason, execution_mode,
			batch_column, batch_size, status, created_by
		) VALUES (
			$1, (SELECT COALESCE(MAX(version), 0) + 1 FROM migrations WHERE database_id = $1),
			1, $2, $3, NULLIF($4, ''),
			$5, NULLIF($6, ''), $7,
			NULLIF($8, ''), $9, 'draft', $10
		) RETURNING id`,
		databaseID, strings.TrimSpace(in.Title), in.SQL, in.SQLDown,
		in.IsIrreversible, strings.TrimSpace(in.IrreversibleReason), string(mode),
		batchCol, batchSizePtr, userID,
	).Scan(&migID)
	if err != nil {
		return nil, nil, err
	}

	var jobID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO migration_jobs (
			database_id, migration_id, action, status, sql_snapshot,
			execution_mode, batch_column, batch_size, requested_by
		) VALUES (
			$1, $2, 'apply', 'draft', $3,
			$4, NULLIF($5, ''), $6, $7
		) RETURNING id`,
		databaseID, migID, in.SQL, string(mode), batchCol, batchSizePtr, userID,
	).Scan(&jobID)
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}

	mig, err := r.GetM2(ctx, migID)
	if err != nil {
		return nil, nil, err
	}
	job, err := r.GetJob(ctx, databaseID, jobID)
	if err != nil {
		return nil, nil, err
	}
	return mig, job, nil
}

func (r *PGRepository) UpdateDraftWithJob(ctx context.Context, databaseID, migrationID, userID int64, in MigrationInputM2) (*Migration, *Job, error) {
	if err := ValidateMigrationInputM2(in); err != nil {
		return nil, nil, err
	}
	mode := in.ExecutionMode
	if mode == "" {
		mode = ExecutionModeStandard
	}
	batchCol := strings.TrimSpace(in.BatchColumn)
	var batchSizePtr *int
	if mode == ExecutionModeBatch && in.BatchSize > 0 {
		sz := in.BatchSize
		batchSizePtr = &sz
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	var isImmutable bool
	var status string
	err = tx.QueryRow(ctx, `
		SELECT is_immutable, status
		FROM migrations
		WHERE id = $1 AND database_id = $2
		FOR UPDATE`,
		migrationID, databaseID,
	).Scan(&isImmutable, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if isImmutable {
		return nil, nil, fmt.Errorf("%w: definisi migration yang sudah pernah dieksekusi bersifat immutable dan tidak dapat diubah", apperr.ErrNotExecutable)
	}
	if Status(status) != StatusDraft && Status(status) != StatusFailed {
		return nil, nil, fmt.Errorf("%w: hanya migration berstatus draft atau failed yang dapat diedit", apperr.ErrNotExecutable)
	}

	_, err = tx.Exec(ctx, `
		UPDATE migrations
		SET revision = revision + 1,
		    title = $3,
		    sql = $4,
		    sql_down = NULLIF($5, ''),
		    is_irreversible = $6,
		    irreversible_reason = NULLIF($7, ''),
		    execution_mode = $8,
		    batch_column = NULLIF($9, ''),
		    batch_size = $10,
		    status = 'draft',
		    error_message = NULL
		WHERE id = $1 AND database_id = $2`,
		migrationID, databaseID, strings.TrimSpace(in.Title), in.SQL, in.SQLDown,
		in.IsIrreversible, strings.TrimSpace(in.IrreversibleReason), string(mode),
		batchCol, batchSizePtr,
	)
	if err != nil {
		return nil, nil, err
	}

	var jobID int64
	err = tx.QueryRow(ctx, `
		UPDATE migration_jobs
		SET status = 'draft',
		    sql_snapshot = $3,
		    execution_mode = $4,
		    batch_column = NULLIF($5, ''),
		    batch_size = $6,
		    error_message = NULL
		WHERE id = (
			SELECT id FROM migration_jobs
			WHERE database_id = $1 AND migration_id = $2 AND status IN ('draft', 'failed')
			ORDER BY id DESC LIMIT 1
		)
		RETURNING id`,
		databaseID, migrationID, in.SQL, string(mode), batchCol, batchSizePtr,
	).Scan(&jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO migration_jobs (
				database_id, migration_id, action, status, sql_snapshot,
				execution_mode, batch_column, batch_size, requested_by
			) VALUES (
				$1, $2, 'apply', 'draft', $3,
				$4, NULLIF($5, ''), $6, $7
			) RETURNING id`,
			databaseID, migrationID, in.SQL, string(mode), batchCol, batchSizePtr, userID,
		).Scan(&jobID)
	}
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}

	mig, err := r.GetM2(ctx, migrationID)
	if err != nil {
		return nil, nil, err
	}
	job, err := r.GetJob(ctx, databaseID, jobID)
	if err != nil {
		return nil, nil, err
	}
	return mig, job, nil
}

package migration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrDatabaseLocked = errors.New("database target sedang dikunci oleh eksekusi migration lain")

type LockManager struct {
	db *pgxpool.Pool
}

func NewLockManager(db *pgxpool.Pool) *LockManager {
	return &LockManager{db: db}
}

func (lm *LockManager) AcquireLock(ctx context.Context, databaseID, jobID int64, workerID string, action JobAction) (bool, error) {
	if databaseID <= 0 || jobID <= 0 || workerID == "" {
		return false, fmt.Errorf("parameter AcquireLock tidak valid")
	}

	tx, err := lm.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		INSERT INTO database_migration_locks (database_id, job_id, worker_id, lock_state, acquired_at, heartbeat_at)
		VALUES ($1, $2, $3, 'running', now(), now())
		ON CONFLICT (database_id) DO NOTHING`,
		databaseID, jobID, workerID,
	)
	if err != nil {
		return false, err
	}

	if tag.RowsAffected() == 1 {
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		return true, nil
	}

	canTakeOver := action == JobActionRetryBatch || action == JobActionCancelBatch
	if !canTakeOver {
		return false, ErrDatabaseLocked
	}

	updateTag, err := tx.Exec(ctx, `
		UPDATE database_migration_locks
		SET job_id = $2,
		    worker_id = $3,
		    lock_state = 'running',
		    heartbeat_at = now()
		WHERE database_id = $1
		  AND (lock_state IN ('holding_partial', 'uncertain') OR job_id = $2)`,
		databaseID, jobID, workerID,
	)
	if err != nil {
		return false, err
	}
	if updateTag.RowsAffected() == 0 {
		return false, ErrDatabaseLocked
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (lm *LockManager) Heartbeat(ctx context.Context, databaseID, jobID int64, workerID string) error {
	tag, err := lm.db.Exec(ctx, `
		UPDATE database_migration_locks
		SET heartbeat_at = now()
		WHERE database_id = $1 AND job_id = $2 AND worker_id = $3`,
		databaseID, jobID, workerID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("lock untuk database %d (job %d) tidak ditemukan atau bukan milik worker %s", databaseID, jobID, workerID)
	}
	return nil
}

func (lm *LockManager) FinishOrHoldLock(ctx context.Context, databaseID, jobID int64, finalJobStatus Status) error {
	shouldHoldPartial := finalJobStatus == StatusPartial || finalJobStatus == StatusPartiallyReverted

	if shouldHoldPartial {
		_, err := lm.db.Exec(ctx, `
			UPDATE database_migration_locks
			SET lock_state = $3, heartbeat_at = now()
			WHERE database_id = $1 AND job_id = $2`,
			databaseID, jobID, string(LockStateHoldingPartial),
		)
		return err
	}

	_, err := lm.db.Exec(ctx, `
		DELETE FROM database_migration_locks
		WHERE database_id = $1 AND job_id = $2`,
		databaseID, jobID,
	)
	return err
}

func (lm *LockManager) MarkUncertain(ctx context.Context, databaseID, jobID int64) error {
	_, err := lm.db.Exec(ctx, `
		UPDATE database_migration_locks
		SET lock_state = $3, heartbeat_at = now()
		WHERE database_id = $1 AND job_id = $2`,
		databaseID, jobID, string(LockStateUncertain),
	)
	return err
}

func (lm *LockManager) GetLock(ctx context.Context, databaseID int64) (*DatabaseLock, error) {
	var l DatabaseLock
	var state string
	err := lm.db.QueryRow(ctx, `
		SELECT database_id, job_id, worker_id, lock_state, acquired_at, heartbeat_at
		FROM database_migration_locks
		WHERE database_id = $1`,
		databaseID,
	).Scan(&l.DatabaseID, &l.JobID, &l.WorkerID, &state, &l.AcquiredAt, &l.HeartbeatAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	l.LockState = LockState(state)
	return &l, nil
}

func (lm *LockManager) RecoverStaleLocks(ctx context.Context, staleThreshold time.Duration) (int64, error) {
	cutoff := time.Now().Add(-staleThreshold)
	tag, err := lm.db.Exec(ctx, `
		UPDATE database_migration_locks
		SET lock_state = 'uncertain'
		WHERE lock_state = 'running' AND heartbeat_at < $1`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

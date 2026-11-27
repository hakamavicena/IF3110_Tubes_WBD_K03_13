package migration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"backend/dbengine/pkg/apperr"
	"backend/dbengine/pkg/ports"
	"backend/dbengine/pkg/queue"
	"backend/dbengine/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

type JobCoordinator struct {
	repo              *PGRepository
	lockManager       *LockManager
	queue             queue.Queue
	pools             ports.PoolProvider
	executor          *Executor
	authz             ports.Authorizer
	metrics           *telemetry.Metrics
	execTimeout       time.Duration
	heartbeatInterval time.Duration
	requeueDelay      time.Duration
}

func (jc *JobCoordinator) SetMetrics(m *telemetry.Metrics) {
	jc.metrics = m
}

func NewJobCoordinator(
	repo *PGRepository,
	lockManager *LockManager,
	q queue.Queue,
	pools ports.PoolProvider,
	executor *Executor,
	authz ports.Authorizer,
	execTimeout time.Duration,
) *JobCoordinator {
	if execTimeout <= 0 {
		execTimeout = 30 * time.Second
	}
	return &JobCoordinator{
		repo:              repo,
		lockManager:       lockManager,
		queue:             q,
		pools:             pools,
		executor:          executor,
		authz:             authz,
		execTimeout:       execTimeout,
		heartbeatInterval: 3 * time.Second,
		requeueDelay:      50 * time.Millisecond,
	}
}

func (jc *JobCoordinator) SetRequeueDelay(d time.Duration) {
	jc.requeueDelay = d
}

func (jc *JobCoordinator) EnqueueJob(ctx context.Context, userID, databaseID, jobID int64, traceParent string) (*Job, error) {
	if jc.authz != nil {
		canEdit, err := jc.authz.CanEdit(ctx, userID, databaseID)
		if err != nil {
			return nil, err
		}
		if !canEdit {
			return nil, fmt.Errorf("%w: user %d does not have edit access to database %d", apperr.ErrForbidden, userID, databaseID)
		}
	}

	job, err := jc.repo.MarkJobQueued(ctx, databaseID, jobID, traceParent)
	if err != nil {
		return nil, err
	}

	msg := queue.JobMessage{
		JobID:       job.ID,
		DatabaseID:  job.DatabaseID,
		TraceParent: traceParent,
		EnqueuedAt:  time.Now().UTC(),
	}

	if err := jc.queue.Publish(ctx, msg); err != nil {
		return nil, fmt.Errorf("gagal mengirim job ke antrean: %w", err)
	}
	if jc.metrics != nil {
		jc.metrics.JobEntered(string(StatusPendingQueue))
	}
	slog.InfoContext(ctx, "job dimasukkan ke antrean",
		"event", "job.enqueue",
		"status", string(job.Status),
		"database_id", databaseID,
		"job_id", job.ID,
		"action", string(job.Action),
	)
	return job, nil
}

func (jc *JobCoordinator) GetJobStatus(ctx context.Context, userID, databaseID, jobID int64) (*Job, error) {
	if jc.authz != nil {
		canView, err := jc.authz.CanView(ctx, userID, databaseID)
		if err != nil {
			return nil, err
		}
		if !canView {
			return nil, fmt.Errorf("%w: user %d does not have view access to database %d", apperr.ErrForbidden, userID, databaseID)
		}
	}
	return jc.repo.GetJob(ctx, databaseID, jobID)
}

func (jc *JobCoordinator) RequestBatchRecovery(
	ctx context.Context,
	userID, databaseID, migrationID int64,
	action JobAction,
) (*Job, error) {
	if jc.authz != nil {
		canEdit, err := jc.authz.CanEdit(ctx, userID, databaseID)
		if err != nil {
			return nil, err
		}
		if !canEdit {
			return nil, fmt.Errorf("%w: user %d does not have edit access to database %d", apperr.ErrForbidden, userID, databaseID)
		}
	}
	return jc.repo.CreateBatchRecoveryJob(ctx, databaseID, migrationID, userID, action)
}

// RequestRollbackForward membuat job rollback/forward berstatus draft. Eksekusi
// baru terjadi setelah approval semua group leader dan enqueue eksplisit.
func (jc *JobCoordinator) RequestRollbackForward(
	ctx context.Context,
	userID, databaseID, migrationID int64,
	action JobAction,
) (*Migration, *Job, error) {
	if jc.authz != nil {
		canEdit, err := jc.authz.CanEdit(ctx, userID, databaseID)
		if err != nil {
			return nil, nil, err
		}
		if !canEdit {
			return nil, nil, fmt.Errorf("%w: user %d does not have edit access to database %d", apperr.ErrForbidden, userID, databaseID)
		}
	}
	return jc.repo.CreateRollbackForwardJob(ctx, databaseID, migrationID, userID, action)
}

func (jc *JobCoordinator) ListMigrationsM2(ctx context.Context, userID, databaseID int64) ([]Migration, bool, *DatabaseLock, error) {
	if jc.authz != nil {
		canView, err := jc.authz.CanView(ctx, userID, databaseID)
		if err != nil {
			return nil, false, nil, err
		}
		if !canView {
			return nil, false, nil, apperr.ErrForbidden
		}
	}
	items, err := jc.repo.ListM2(ctx, databaseID)
	if err != nil {
		return nil, false, nil, err
	}
	canEdit := false
	if jc.authz != nil {
		canEdit, _ = jc.authz.CanEdit(ctx, userID, databaseID)
	}
	lock, _ := jc.lockManager.GetLock(ctx, databaseID)
	return items, canEdit, lock, nil
}

func (jc *JobCoordinator) GetMigrationDetailM2(ctx context.Context, userID, databaseID, migrationID int64) (*Migration, []Job, bool, *DatabaseLock, error) {
	if jc.authz != nil {
		canView, err := jc.authz.CanView(ctx, userID, databaseID)
		if err != nil {
			return nil, nil, false, nil, err
		}
		if !canView {
			return nil, nil, false, nil, apperr.ErrForbidden
		}
	}
	m, err := jc.repo.GetM2(ctx, migrationID)
	if err != nil {
		return nil, nil, false, nil, err
	}
	if m.DatabaseID != databaseID {
		return nil, nil, false, nil, apperr.ErrNotFound
	}
	jobs, err := jc.repo.ListJobsByMigration(ctx, databaseID, migrationID)
	if err != nil {
		return nil, nil, false, nil, err
	}
	canEdit := false
	if jc.authz != nil {
		canEdit, _ = jc.authz.CanEdit(ctx, userID, databaseID)
	}
	lock, _ := jc.lockManager.GetLock(ctx, databaseID)
	return m, jobs, canEdit, lock, nil
}

func (jc *JobCoordinator) CreateMigrationM2(ctx context.Context, userID, databaseID int64, in MigrationInputM2) (*Migration, *Job, error) {
	if jc.authz != nil {
		canEdit, err := jc.authz.CanEdit(ctx, userID, databaseID)
		if err != nil {
			return nil, nil, err
		}
		if !canEdit {
			return nil, nil, apperr.ErrForbidden
		}
	}
	return jc.repo.CreateMigrationWithJob(ctx, databaseID, userID, in)
}

func (jc *JobCoordinator) UpdateMigrationM2(ctx context.Context, userID, databaseID, migrationID int64, in MigrationInputM2) (*Migration, *Job, error) {
	if jc.authz != nil {
		canEdit, err := jc.authz.CanEdit(ctx, userID, databaseID)
		if err != nil {
			return nil, nil, err
		}
		if !canEdit {
			return nil, nil, apperr.ErrForbidden
		}
	}
	return jc.repo.UpdateDraftWithJob(ctx, databaseID, migrationID, userID, in)
}

func (jc *JobCoordinator) RecoverOnStartup(ctx context.Context, staleThreshold time.Duration) (int64, error) {
	if staleThreshold <= 0 {
		staleThreshold = 15 * time.Second
	}
	staleLocks, err := jc.lockManager.RecoverStaleLocks(ctx, staleThreshold)
	if err != nil {
		return 0, err
	}

	// Hanya job 'running' yang lock-nya sudah basi (RecoverStaleLocks menandainya
	// 'uncertain') atau tidak punya lock sama sekali yang dianggap tertinggal.
	// Job yang heartbeat-nya masih segar milik instance lain dan tidak boleh disentuh,
	// sehingga menyalakan instance kedua tidak mematikan job yang sedang berjalan.
	rows, err := jc.repo.db.Query(ctx, `
		SELECT j.id, j.database_id, j.execution_mode, j.committed_batches
		FROM migration_jobs j
		LEFT JOIN database_migration_locks l
		       ON l.database_id = j.database_id AND l.job_id = j.id
		WHERE j.status = 'running'
		  AND (l.job_id IS NULL OR l.lock_state = 'uncertain')`)
	if err != nil {
		return staleLocks, err
	}
	defer rows.Close()

	type stuckJob struct {
		id               int64
		databaseID       int64
		execMode         string
		committedBatches int
	}
	var stuck []stuckJob
	for rows.Next() {
		var sj stuckJob
		if err := rows.Scan(&sj.id, &sj.databaseID, &sj.execMode, &sj.committedBatches); err != nil {
			return staleLocks, err
		}
		stuck = append(stuck, sj)
	}
	if err := rows.Err(); err != nil {
		return staleLocks, err
	}

	var recovered int64
	for _, sj := range stuck {
		job, err := jc.repo.GetJob(ctx, sj.databaseID, sj.id)
		if err != nil {
			continue
		}
		newStatus := StatusFailed
		if ExecutionMode(sj.execMode) == ExecutionModeBatch && sj.committedBatches > 0 {
			newStatus = StatusPartial
		}
		progress := BatchProgress{
			CommittedBatches: job.CommittedBatches,
			CommittedRows:    job.CommittedRows,
			LastCursor:       job.LastCursor,
		}
		_ = jc.repo.FinalizeJobExecution(ctx, job, newStatus, progress, 0, "interrupted: worker restarted during execution")
		_ = jc.lockManager.FinishOrHoldLock(ctx, sj.databaseID, sj.id, newStatus)
		recovered++
	}
	return recovered, nil
}

func (jc *JobCoordinator) StartWorkers(ctx context.Context, workerCount int) *sync.WaitGroup {
	if workerCount <= 0 {
		workerCount = 2
	}
	var wg sync.WaitGroup
	for i := 1; i <= workerCount; i++ {
		wg.Add(1)
		workerID := fmt.Sprintf("worker-%d", i)
		go func(wID string) {
			defer wg.Done()
			jc.workerLoop(ctx, wID)
		}(workerID)
	}
	return &wg
}

func (jc *JobCoordinator) workerLoop(ctx context.Context, workerID string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := jc.queue.Consume(ctx, 2*time.Second)
		if err != nil {
			if errors.Is(err, queue.ErrQueueEmpty) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			slog.Warn("queue consume error", "worker_id", workerID, "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
				continue
			}
		}
		if msg == nil {
			continue
		}

		if err := jc.ProcessMessage(ctx, workerID, *msg); err != nil {
			slog.Error("failed processing job message", "worker_id", workerID, "job_id", msg.JobID, "database_id", msg.DatabaseID, "err", err)
		}
	}
}

// ProcessMessage melanjutkan trace dari API (traceparent yang dibawa pesan queue)
// lalu menjalankan job di dalam span worker.
func (jc *JobCoordinator) ProcessMessage(ctx context.Context, workerID string, msg queue.JobMessage) error {
	ctx = telemetry.ExtractTraceParent(ctx, msg.TraceParent)
	ctx, span := telemetry.StartSpan(ctx, "worker.process_job",
		attribute.Int64("job.id", msg.JobID),
		attribute.Int64("database.id", msg.DatabaseID),
	)
	err := jc.processMessage(ctx, workerID, msg)
	telemetry.EndSpan(span, err)
	return err
}

func (jc *JobCoordinator) processMessage(ctx context.Context, workerID string, msg queue.JobMessage) error {
	job, err := jc.repo.GetJob(ctx, msg.DatabaseID, msg.JobID)
	if err != nil {
		return err
	}

	if job.Status != StatusPendingQueue && job.Status != StatusApproved && job.Status != StatusRunning {
		return nil
	}

	lockStart := time.Now()
	lockCtx, lockSpan := telemetry.StartSpan(ctx, "lock.acquire",
		attribute.Int64("database.id", job.DatabaseID),
		attribute.Int64("job.id", job.ID),
		attribute.String("worker.id", workerID),
	)
	acquired, err := jc.lockManager.AcquireLock(lockCtx, job.DatabaseID, job.ID, workerID, job.Action)
	telemetry.EndSpan(lockSpan, err)
	if err != nil {
		if errors.Is(err, ErrDatabaseLocked) {
			if jc.metrics != nil {
				jc.metrics.JobRetried("lock_busy")
			}
			if jc.requeueDelay > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(jc.requeueDelay):
				}
			}
			return jc.queue.Publish(ctx, msg)
		}
		return err
	}
	if !acquired {
		return jc.queue.Publish(ctx, msg)
	}

	if jc.metrics != nil {
		jc.metrics.ObserveLockWait(time.Since(lockStart))
		if job.QueuedAt != nil {
			jc.metrics.ObserveQueueWait(time.Since(*job.QueuedAt))
		} else if !msg.EnqueuedAt.IsZero() {
			jc.metrics.ObserveQueueWait(time.Since(msg.EnqueuedAt))
		}
		jc.metrics.JobLeft(string(StatusPendingQueue))
		jc.metrics.JobEntered(string(StatusRunning))
	}

	job, err = jc.repo.MarkJobRunning(ctx, job.DatabaseID, job.ID)
	if err != nil {
		if jc.metrics != nil {
			jc.metrics.JobLeft(string(StatusRunning))
		}
		_ = jc.lockManager.FinishOrHoldLock(ctx, msg.DatabaseID, msg.JobID, StatusFailed)
		return err
	}

	hbCtx, cancelHB := context.WithCancel(ctx)
	defer cancelHB()
	go jc.runHeartbeat(hbCtx, job.DatabaseID, job.ID, workerID)

	runCtx, cancelRun := context.WithTimeout(context.WithoutCancel(ctx), jc.execTimeout)
	defer cancelRun()

	start := time.Now()
	progress, finalStatus, runErr := jc.executeJobSQL(runCtx, job)
	execDuration := time.Since(start)
	durationMs := int(execDuration.Milliseconds())

	if jc.metrics != nil {
		jc.metrics.JobLeft(string(StatusRunning))
		jc.metrics.JobEntered(string(finalStatus))
		jc.metrics.JobFinished(string(finalStatus))
		jc.metrics.ObserveMigration(string(job.ExecutionMode), string(job.Action), execDuration)
	}

	errMsg := ""
	if runErr != nil {
		errMsg = describeError(runErr)
	}

	slog.InfoContext(ctx, "eksekusi job selesai",
		"event", "job.finish",
		"status", string(finalStatus),
		"database_id", job.DatabaseID,
		"migration_id", job.MigrationID,
		"job_id", job.ID,
		"action", string(job.Action),
		"mode", string(job.ExecutionMode),
		"committed_batches", progress.CommittedBatches,
		"committed_rows", progress.CommittedRows,
		"duration_ms", durationMs,
	)

	finalizeCtx := context.WithoutCancel(ctx)
	if err := jc.repo.FinalizeJobExecution(finalizeCtx, job, finalStatus, progress, durationMs, errMsg); err != nil {
		if jc.metrics != nil {
			jc.metrics.JobRetried("uncertain_commit")
		}
		_ = jc.lockManager.MarkUncertain(finalizeCtx, job.DatabaseID, job.ID)
		return err
	}

	return jc.lockManager.FinishOrHoldLock(finalizeCtx, job.DatabaseID, job.ID, finalStatus)
}

func (jc *JobCoordinator) runHeartbeat(ctx context.Context, databaseID, jobID int64, workerID string) {
	ticker := time.NewTicker(jc.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = jc.lockManager.Heartbeat(ctx, databaseID, jobID, workerID)
		}
	}
}

func (jc *JobCoordinator) executeJobSQL(ctx context.Context, job *Job) (BatchProgress, Status, error) {
	progress := BatchProgress{
		CommittedBatches: job.CommittedBatches,
		CommittedRows:    job.CommittedRows,
		LastCursor:       job.LastCursor,
	}

	pool, err := jc.pools.Pool(ctx, job.DatabaseID)
	if err != nil {
		return progress, StatusFailed, err
	}

	if job.ExecutionMode == ExecutionModeBatch {
		checkpointJobID, err := jc.repo.ResolveCheckpointJobID(ctx, job.DatabaseID, job.MigrationID, job.ID)
		if err != nil {
			return progress, StatusFailed, err
		}
		prevRows := progress.CommittedRows
		onProgress := func(pCtx context.Context, bp BatchProgress) error {
			delta := bp.CommittedRows - prevRows
			if delta < 0 {
				delta = 0
			}
			prevRows = bp.CommittedRows
			if jc.metrics != nil {
				jc.metrics.BatchCommitted(string(job.Action), delta)
			}
			return jc.repo.UpdateJobBatchProgress(pCtx, job.ID, bp)
		}

		if job.Action == JobActionCancelBatch || job.Action == JobActionRollback {
			return jc.executor.RunBatchCancel(ctx, pool, checkpointJobID, job.SQLSnapshot, job.BatchColumn, onProgress)
		}
		return jc.executor.RunBatch(ctx, pool, checkpointJobID, job.SQLSnapshot, job.BatchColumn, job.BatchSize, onProgress)
	}

	if err := jc.executor.Run(ctx, pool, job.SQLSnapshot); err != nil {
		return progress, StatusFailed, err
	}
	progress.CommittedBatches = 1
	if jc.metrics != nil {
		jc.metrics.BatchCommitted(string(job.Action), progress.CommittedRows)
	}
	return progress, StatusSuccess, nil
}

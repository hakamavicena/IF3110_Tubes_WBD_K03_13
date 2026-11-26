package migration

import "time"

type Status string

const (
	StatusDraft             Status = "draft"
	StatusPendingReview     Status = "pending_review"
	StatusApproved          Status = "approved"
	StatusPendingQueue      Status = "pending_queue"
	StatusRunning           Status = "running"
	StatusSuccess           Status = "success"
	StatusFailed            Status = "failed"
	StatusPartial           Status = "partial"
	StatusPartiallyReverted Status = "partially_reverted"
)

type ExecutionMode string

const (
	ExecutionModeStandard ExecutionMode = "standard"
	ExecutionModeBatch    ExecutionMode = "batch"
)

type ActiveState string

const (
	ActiveStateNotApplied        ActiveState = "not_applied"
	ActiveStateApplied           ActiveState = "applied"
	ActiveStateRolledBack        ActiveState = "rolled_back"
	ActiveStatePartial           ActiveState = "partial"
	ActiveStatePartiallyReverted ActiveState = "partially_reverted"
)

type JobAction string

const (
	JobActionApply       JobAction = "apply"
	JobActionRollback    JobAction = "rollback"
	JobActionForward     JobAction = "forward"
	JobActionRetryBatch  JobAction = "retry_batch"
	JobActionCancelBatch JobAction = "cancel_batch"
)

type LockState string

const (
	LockStateRunning        LockState = "running"
	LockStateHoldingPartial LockState = "holding_partial"
	LockStateUncertain      LockState = "uncertain"
)

type Migration struct {
	ID                 int64
	DatabaseID         int64
	Version            int
	Revision           int
	Title              string
	SQL                string
	SQLDown            string
	IsIrreversible     bool
	IrreversibleReason string
	ExecutionMode      ExecutionMode
	BatchColumn        string
	BatchSize          int
	IsImmutable        bool
	ActiveState        ActiveState
	ActiveExecutionID  *int64
	Status             Status
	ErrorMessage       string
	CreatedBy          int64
	CreatedByName      string
	CreatedAt          time.Time
	ExecutedBy         *int64
	ExecutedByName     *string
	ExecutedAt         *time.Time
	DurationMs         *int
}

type Job struct {
	ID                 int64
	DatabaseID         int64
	MigrationID        int64
	Action             JobAction
	Status             Status
	RevertsExecutionID *int64
	SQLSnapshot        string
	ExecutionMode      ExecutionMode
	BatchColumn        string
	BatchSize          int
	CommittedBatches   int
	CommittedRows      int64
	LastCursor         string
	RetryCount         int
	ErrorMessage       string
	TraceParent        string
	RequestedBy        int64
	CreatedAt          time.Time
	QueuedAt           *time.Time
	StartedAt          *time.Time
	FinishedAt         *time.Time
	DurationMs         *int
}

type DatabaseLock struct {
	DatabaseID  int64
	JobID       int64
	WorkerID    string
	LockState   LockState
	AcquiredAt  time.Time
	HeartbeatAt time.Time
}

type BatchCheckpoint struct {
	JobID        int64
	BatchNumber  int
	StartCursor  string
	EndCursor    string
	RowsAffected int64
	UndoPayload  string
	CommittedAt  time.Time
	RevertedAt   *time.Time
}

package migration

import "time"

type Status string

const (
	StatusDraft   Status = "draft"
	StatusRunning Status = "running"
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
)

type Migration struct {
	ID                 int64
	DatabaseID         int64
	Version            int
	Title              string
	SQL                string
	SQLDown            string
	IsIrreversible     bool
	IrreversibleReason string
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

type ExecutionAction string

const (
	ActionApply    ExecutionAction = "apply"
	ActionRollback ExecutionAction = "rollback"
	ActionForward  ExecutionAction = "forward"
)

type Execution struct {
	ExecutionID         int64
	DatabaseID          int64
	MigrationID         int64
	JobID               *int64
	Version             int
	Revision            int
	Title               string
	Action              ExecutionAction
	SQLExecuted         string
	ExecutedBy          int64
	ExecutedAt          time.Time
	DurationMs          int
	RowsAffected        int64
	Status              Status
	RevertedExecutionID *int64
}

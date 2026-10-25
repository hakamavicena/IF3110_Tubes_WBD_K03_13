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
	ID             int64
	DatabaseID     int64
	Version        int
	Title          string
	SQL            string
	Status         Status
	ErrorMessage   string
	CreatedBy      int64
	CreatedByName  string
	CreatedAt      time.Time
	ExecutedBy     *int64
	ExecutedByName *string
	ExecutedAt     *time.Time
	DurationMs     *int
}

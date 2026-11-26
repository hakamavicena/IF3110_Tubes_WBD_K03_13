package accesscontrol

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"backend/dbengine/pkg/ports"
)

const (
	JobStatusDraft         = "draft"
	JobStatusPendingReview = "pending_review"
	JobStatusApproved      = "approved"
)

var (
	ErrJobNotFound     = errors.New("job tidak ditemukan")
	ErrInvalidJobState = errors.New("status job tidak sesuai untuk aksi ini")
	ErrNotLeader       = errors.New("hanya leader group yang berwenang dapat memberikan approval")
	ErrGroupNoLeader   = errors.New("ada group dengan akses ke database ini yang belum memiliki leader aktif")
	ErrForbidden       = errors.New("akses ditolak")
)

type Job struct {
	ID          int64     `json:"id"`
	DatabaseID  int64     `json:"database_id"`
	MigrationID int64     `json:"migration_id"`
	Action      string    `json:"action"`
	Status      string    `json:"status"`
	RequestedBy int64     `json:"requested_by"`
	CreatedAt   time.Time `json:"created_at"`
}

type RequiredApproval struct {
	GroupID        int        `json:"group_id"`
	GroupName      string     `json:"group_name"`
	AccessLevel    int        `json:"access_level"` 
	LeaderID       *int       `json:"leader_id"`
	LeaderUsername string     `json:"leader_username,omitempty"`
	LeaderActive   bool       `json:"leader_active"`
	Approved       bool       `json:"approved"`
	ApprovedAt     *time.Time `json:"approved_at,omitempty"`
}

type ApprovalStatus struct {
	JobID         int64              `json:"job_id"`
	DatabaseID    int64              `json:"database_id"`
	Action        string             `json:"action"`
	Status        string             `json:"status"`
	AllApproved   bool               `json:"all_approved"`
	Required      []RequiredApproval `json:"required"`
	CanApproveFor []int              `json:"can_approve_groups"` 
}

type InboxItem struct {
	JobID        int64         `json:"job_id"`
	DatabaseID   int64         `json:"database_id"`
	DatabaseName string        `json:"database_name"`
	Action       string        `json:"action"`
	Version      int           `json:"migration_version"`
	Title        string        `json:"migration_title"`
	RequestedBy  string        `json:"requested_by"`
	CreatedAt    time.Time     `json:"created_at"`
	Groups       []LeaderGroup `json:"waiting_groups"`
}

type LeaderGroup struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type ApprovalTx interface {
	GetJob(ctx context.Context) (*Job, error)
	Required(ctx context.Context) ([]RequiredApproval, error)
	InsertApproval(ctx context.Context, groupID, leaderID int) error
	SetStatus(ctx context.Context, from, to string) (bool, error)
}

type ApprovalStore interface {
	WithJobLock(ctx context.Context, databaseID, jobID int64, fn func(tx ApprovalTx) error) error
	PendingForLeader(ctx context.Context, userID int64) ([]InboxItem, error)
	ReadJob(ctx context.Context, databaseID, jobID int64) (*Job, []RequiredApproval, error)
}

type ApprovalService struct {
	store ApprovalStore
	authz ports.Authorizer
	now   func() time.Time
}

func NewApprovalService(store ApprovalStore, authz ports.Authorizer) *ApprovalService {
	return &ApprovalService{store: store, authz: authz, now: time.Now}
}

func allApproved(req []RequiredApproval) bool {
	if len(req) == 0 {
		return false
	}
	for _, r := range req {
		if !r.Approved {
			return false
		}
	}
	return true
}

func groupsMissingLeader(req []RequiredApproval) []string {
	var names []string
	for _, r := range req {
		if r.LeaderID == nil || !r.LeaderActive {
			names = append(names, r.GroupName)
		}
	}
	sort.Strings(names)
	return names
}

func canApproveFor(req []RequiredApproval, userID int) []int {
	out := []int{}
	for _, r := range req {
		if !r.Approved && r.LeaderID != nil && r.LeaderActive && *r.LeaderID == userID {
			out = append(out, r.GroupID)
		}
	}
	return out
}

func buildStatus(job *Job, req []RequiredApproval, userID int) *ApprovalStatus {
	if req == nil {
		req = []RequiredApproval{}
	}
	return &ApprovalStatus{
		JobID: job.ID, DatabaseID: job.DatabaseID, Action: job.Action, Status: job.Status,
		AllApproved: allApproved(req), Required: req, CanApproveFor: canApproveFor(req, userID),
	}
}

func (s *ApprovalService) SubmitReview(ctx context.Context, userID, databaseID, jobID int64) (*ApprovalStatus, error) {
	ok, err := s.authz.CanEdit(ctx, userID, databaseID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}

	var status *ApprovalStatus
	err = s.store.WithJobLock(ctx, databaseID, jobID, func(tx ApprovalTx) error {
		job, err := tx.GetJob(ctx)
		if err != nil {
			return err
		}
		if job.Status != JobStatusDraft {
			return fmt.Errorf("%w: status saat ini %q, harus %q", ErrInvalidJobState, job.Status, JobStatusDraft)
		}
		req, err := tx.Required(ctx)
		if err != nil {
			return err
		}
		if len(req) == 0 {
			return fmt.Errorf("%w: tidak ada group yang memiliki akses ke database ini", ErrGroupNoLeader)
		}
		if missing := groupsMissingLeader(req); len(missing) > 0 {
			return fmt.Errorf("%w: %s", ErrGroupNoLeader, strings.Join(missing, ", "))
		}
		moved, err := tx.SetStatus(ctx, JobStatusDraft, JobStatusPendingReview)
		if err != nil {
			return err
		}
		if !moved {
			return fmt.Errorf("%w: job sudah berubah status", ErrInvalidJobState)
		}
		job.Status = JobStatusPendingReview
		status = buildStatus(job, req, int(userID))
		return nil
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "job diajukan untuk review", "event", "approval.submit_review", "status", "success",
		"actor_id", userID, "database_id", databaseID, "job_id", jobID, "required_groups", len(status.Required))
	return status, nil
}

func (s *ApprovalService) Approve(ctx context.Context, userID, databaseID, jobID int64, groupID int) (*ApprovalStatus, error) {
	var status *ApprovalStatus
	var approvedGroups []int
	err := s.store.WithJobLock(ctx, databaseID, jobID, func(tx ApprovalTx) error {
		job, err := tx.GetJob(ctx)
		if err != nil {
			return err
		}
		req, err := tx.Required(ctx)
		if err != nil {
			return err
		}

		var mine []int
		for _, r := range req {
			if r.LeaderID != nil && r.LeaderActive && *r.LeaderID == int(userID) {
				mine = append(mine, r.GroupID)
			}
		}
		if len(mine) == 0 {
			return ErrNotLeader
		}
		if job.Status != JobStatusPendingReview {
			return fmt.Errorf("%w: status saat ini %q, harus %q", ErrInvalidJobState, job.Status, JobStatusPendingReview)
		}
		targets := mine
		if groupID > 0 {
			found := false
			for _, g := range mine {
				if g == groupID {
					found = true
				}
			}
			if !found {
				return ErrNotLeader
			}
			targets = []int{groupID}
		}

		for _, g := range targets {
			already := false
			for _, r := range req {
				if r.GroupID == g && r.Approved {
					already = true
				}
			}
			if already {
				continue // idempoten
			}
			if err := tx.InsertApproval(ctx, g, int(userID)); err != nil {
				return err
			}
			approvedGroups = append(approvedGroups, g)
		}

		req, err = tx.Required(ctx)
		if err != nil {
			return err
		}
		if allApproved(req) {
			moved, err := tx.SetStatus(ctx, JobStatusPendingReview, JobStatusApproved)
			if err != nil {
				return err
			}
			if !moved {
				return fmt.Errorf("%w: job sudah berubah status", ErrInvalidJobState)
			}
			job.Status = JobStatusApproved
		}
		status = buildStatus(job, req, int(userID))
		return nil
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "approval leader dicatat", "event", "approval.approve", "status", status.Status,
		"actor_id", userID, "database_id", databaseID, "job_id", jobID, "approved_groups", approvedGroups,
		"all_approved", status.AllApproved)
	return status, nil
}

func (s *ApprovalService) Status(ctx context.Context, userID, databaseID, jobID int64) (*ApprovalStatus, error) {
	ok, err := s.authz.CanView(ctx, userID, databaseID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	job, req, err := s.store.ReadJob(ctx, databaseID, jobID)
	if err != nil {
		return nil, err
	}
	return buildStatus(job, req, int(userID)), nil
}

func (s *ApprovalService) Inbox(ctx context.Context, userID int64) ([]InboxItem, error) {
	items, err := s.store.PendingForLeader(ctx, userID)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []InboxItem{}
	}
	return items, nil
}

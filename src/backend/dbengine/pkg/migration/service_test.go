package migration

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/dbengine/pkg/apperr"
	"backend/dbengine/pkg/ports"

	"github.com/jackc/pgx/v5/pgxpool"
)

type serviceTestAuth struct{ view, edit bool }

func (a serviceTestAuth) CanView(context.Context, int64, int64) (bool, error) {
	return a.view, nil
}
func (a serviceTestAuth) CanEdit(context.Context, int64, int64) (bool, error) {
	return a.edit, nil
}
func (serviceTestAuth) AccessibleDatabases(context.Context, int64) ([]ports.DatabaseAccess, error) {
	return nil, nil
}

type serviceTestPools struct{}

func (serviceTestPools) Pool(context.Context, int64) (*pgxpool.Pool, error) {
	return nil, nil
}

type serviceTestRunner struct{ err error }

func (r serviceTestRunner) Run(context.Context, *pgxpool.Pool, string) error { return r.err }

type serviceTestRepo struct {
	migration           *Migration
	finished            bool
	execution           *Execution
	rollbackSQL         string
	rollbackExecutionID int64
	rollbackClaimed     bool
	forwardSQL          string
	forwardExecutionID  int64
	forwardClaimed      bool
}

func (r *serviceTestRepo) Create(context.Context, *Migration) (*Migration, error) {
	panic("unexpected Create")
}
func (r *serviceTestRepo) Get(context.Context, int64) (*Migration, error) {
	if r.migration == nil {
		return nil, apperr.ErrNotFound
	}
	copy := *r.migration
	return &copy, nil
}
func (r *serviceTestRepo) List(context.Context, int64) ([]Migration, error) { return nil, nil }

func (r *serviceTestRepo) RecordExecution(
	ctx context.Context,
	execution *Execution,
) (*Execution, error) {
	recorded := *execution
	r.execution = &recorded
	return &recorded, nil
}

func (r *serviceTestRepo) ClaimRollback(ctx context.Context, id, databaseID int64) (sqlDown string, revertedExecutionID int64, ok bool, err error) {
	return r.rollbackSQL, r.rollbackExecutionID, r.rollbackClaimed, nil
}

func (r *serviceTestRepo) ClaimForward(
	ctx context.Context,
	id, databaseID int64,
) (string, int64, bool, error) {
	return r.forwardSQL, r.forwardExecutionID, r.forwardClaimed, nil
}

func (r *serviceTestRepo) UpdateDraft(context.Context, int64, int64, string, string, string, bool, string) error {
	panic("unexpected UpdateDraft")
}
func (r *serviceTestRepo) Claim(context.Context, int64, int64) (string, bool, error) {
	return r.migration.SQL, true, nil
}
func (r *serviceTestRepo) Finish(_ context.Context, _ int64, status Status, message string, ms int) error {
	r.finished = true
	r.migration.Status = status
	r.migration.ErrorMessage = message
	r.migration.DurationMs = &ms
	return nil
}
func (r *serviceTestRepo) RecoverStuck(context.Context) (int64, error) { return 0, nil }

func TestGetHidesMigrationFromOtherDatabase(t *testing.T) {
	repo := &serviceTestRepo{migration: &Migration{ID: 5, DatabaseID: 99}}
	svc := NewService(repo, serviceTestAuth{view: true}, serviceTestPools{}, serviceTestRunner{}, time.Second)
	if _, err := svc.Get(context.Background(), 1, 10, 5); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("expected not found for cross-database migration, got %v", err)
	}
}

func TestExecuteRequiresEditBeforeLoadingMigration(t *testing.T) {
	repo := &serviceTestRepo{}
	svc := NewService(repo, serviceTestAuth{view: true}, serviceTestPools{}, serviceTestRunner{}, time.Second)
	if _, err := svc.Execute(context.Background(), 1, 10, 1); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestExecuteSQLFailureIsStoredAsFailedStatus(t *testing.T) {
	repo := &serviceTestRepo{migration: &Migration{
		ID: 1, DatabaseID: 10, SQL: "invalid SQL", Status: StatusDraft,
	}}
	svc := NewService(repo, serviceTestAuth{view: true, edit: true}, serviceTestPools{},
		serviceTestRunner{err: errors.New("target unavailable")}, time.Second)
	got, err := svc.Execute(context.Background(), 1, 10, 1)
	if err != nil {
		t.Fatalf("SQL execution failure should be persisted, not returned: %v", err)
	}
	if !repo.finished || got.Status != StatusFailed || got.ErrorMessage != "eksekusi gagal" {
		t.Fatalf("unexpected migration result: %+v", got)
	}
}

func TestExecuteRejectsInvalidTimeoutBeforeClaim(t *testing.T) {
	repo := &serviceTestRepo{migration: &Migration{ID: 1, DatabaseID: 10, SQL: "SELECT 1"}}
	svc := NewService(repo, serviceTestAuth{view: true, edit: true}, serviceTestPools{}, serviceTestRunner{}, 0)
	if _, err := svc.Execute(context.Background(), 1, 10, 1); err == nil {
		t.Fatal("expected timeout configuration error")
	}
	if repo.finished {
		t.Fatal("invalid timeout unexpectedly ran")
	}
}

func TestRollbackRecordsDownExecution(t *testing.T) {
	repo := &serviceTestRepo{
		migration: &Migration{
			ID:         1,
			DatabaseID: 10,
			Version:    2,
			Title:      "second migration",
			Status:     StatusSuccess,
		},
		rollbackSQL:         "DROP TABLE items",
		rollbackExecutionID: 42,
		rollbackClaimed:     true,
	}
	svc := NewService(
		repo,
		serviceTestAuth{view: true, edit: true},
		serviceTestPools{},
		serviceTestRunner{},
		time.Second,
	)

	got, err := svc.Rollback(context.Background(), 7, 10, 1)
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if got.Status != StatusSuccess {
		t.Fatalf("status = %q, want %q", got.Status, StatusSuccess)
	}
	if !repo.finished {
		t.Fatal("rollback result was not finalized")
	}
	if repo.execution == nil {
		t.Fatal("rollback execution was not recorded")
	}
	if repo.execution.Action != ActionRollback {
		t.Errorf("action = %q, want %q", repo.execution.Action, ActionRollback)
	}
	if repo.execution.SQLExecuted != repo.rollbackSQL {
		t.Errorf("SQLExecuted = %q, want %q", repo.execution.SQLExecuted, repo.rollbackSQL)
	}
	if repo.execution.RevertedExecutionID == nil ||
		*repo.execution.RevertedExecutionID != repo.rollbackExecutionID {
		t.Errorf("RevertedExecutionID = %v, want %d",
			repo.execution.RevertedExecutionID, repo.rollbackExecutionID)
	}
}

func TestForwardRecordsUpExecution(t *testing.T) {
	repo := &serviceTestRepo{
		migration: &Migration{
			ID:         1,
			DatabaseID: 10,
			Version:    2,
			Title:      "second migration",
			Status:     StatusSuccess,
		},
		forwardSQL:         "CREATE TABLE items (id int)",
		forwardExecutionID: 42,
		forwardClaimed:     true,
	}
	svc := NewService(
		repo,
		serviceTestAuth{view: true, edit: true},
		serviceTestPools{},
		serviceTestRunner{},
		time.Second,
	)

	got, err := svc.Forward(context.Background(), 7, 10, 1)
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if got.Status != StatusSuccess {
		t.Fatalf("status = %q, want %q", got.Status, StatusSuccess)
	}
	if !repo.finished {
		t.Fatal("forward result was not finalized")
	}
	if repo.execution == nil {
		t.Fatal("forward execution was not recorded")
	}
	if repo.execution.Action != ActionForward {
		t.Errorf("action = %q, want %q", repo.execution.Action, ActionForward)
	}
	if repo.execution.SQLExecuted != repo.forwardSQL {
		t.Errorf("SQLExecuted = %q, want %q", repo.execution.SQLExecuted, repo.forwardSQL)
	}
	if repo.execution.RevertedExecutionID == nil ||
		*repo.execution.RevertedExecutionID != repo.forwardExecutionID {
		t.Errorf("RevertedExecutionID = %v, want %d",
			repo.execution.RevertedExecutionID, repo.forwardExecutionID)
	}
}

func TestForwardRecordsSQLFailure(t *testing.T) {
	repo := &serviceTestRepo{
		migration: &Migration{
			ID:         1,
			DatabaseID: 10,
			Version:    2,
			Title:      "second migration",
			Status:     StatusSuccess,
		},
		forwardSQL:         "invalid SQL",
		forwardExecutionID: 42,
		forwardClaimed:     true,
	}
	svc := NewService(
		repo,
		serviceTestAuth{view: true, edit: true},
		serviceTestPools{},
		serviceTestRunner{err: errors.New("target unavailable")},
		time.Second,
	)

	got, err := svc.Forward(context.Background(), 7, 10, 1)
	if err != nil {
		t.Fatalf("Forward() should persist SQL failure, got error %v", err)
	}
	if got.Status != StatusFailed {
		t.Fatalf("status = %q, want %q", got.Status, StatusFailed)
	}
	if repo.execution == nil || repo.execution.Status != StatusFailed {
		t.Fatalf("failed forward execution was not recorded: %+v", repo.execution)
	}
}

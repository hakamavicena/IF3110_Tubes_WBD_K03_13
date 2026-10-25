package migration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hakamavicena/if3110-dbengine/internal/apperr"
	"github.com/hakamavicena/if3110-dbengine/internal/dbtarget"
	"github.com/hakamavicena/if3110-dbengine/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Service struct {
	repo        Repository
	authz       ports.Authorizer
	pools       ports.PoolProvider
	runner      SQLRunner
	execTimeout time.Duration
}

func NewService(repo Repository, authz ports.Authorizer, pools ports.PoolProvider, runner SQLRunner, timeout time.Duration) *Service {
	return &Service{repo: repo, authz: authz, pools: pools, runner: runner, execTimeout: timeout}
}

func (s *Service) requireView(ctx context.Context, userID, dbID int64) error {
	if s.authz == nil {
		return fmt.Errorf("migration authorizer is not configured")
	}
	ok, err := s.authz.CanView(ctx, userID, dbID)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.ErrForbidden
	}
	return nil
}

func (s *Service) requireEdit(ctx context.Context, userID, dbID int64) error {
	if s.authz == nil {
		return fmt.Errorf("migration authorizer is not configured")
	}
	ok, err := s.authz.CanEdit(ctx, userID, dbID)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.ErrForbidden
	}
	return nil
}

func (s *Service) List(ctx context.Context, userID, dbID int64) ([]Migration, error) {
	if err := s.requireView(ctx, userID, dbID); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, dbID)
}

func (s *Service) Get(ctx context.Context, userID, dbID, migrationID int64) (*Migration, error) {
	if err := s.requireView(ctx, userID, dbID); err != nil {
		return nil, err
	}
	m, err := s.repo.Get(ctx, migrationID)
	if err != nil {
		return nil, err
	}
	if m.DatabaseID != dbID {
		return nil, apperr.ErrNotFound
	}
	return m, nil
}

func (s *Service) Create(ctx context.Context, userID, dbID int64, title, sql string) (*Migration, error) {
	if err := s.requireEdit(ctx, userID, dbID); err != nil {
		return nil, err
	}
	if err := ValidateInput(title, sql); err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, &Migration{DatabaseID: dbID, Title: strings.TrimSpace(title), SQL: sql, CreatedBy: userID})
}

func (s *Service) Update(ctx context.Context, userID, dbID, migrationID int64, title, sql string) error {
	if err := s.requireEdit(ctx, userID, dbID); err != nil {
		return err
	}
	if err := ValidateInput(title, sql); err != nil {
		return err
	}
	return s.repo.UpdateDraft(ctx, migrationID, dbID, strings.TrimSpace(title), sql)
}

func (s *Service) Execute(ctx context.Context, userID, dbID, migrationID int64) (*Migration, error) {
	if err := s.requireEdit(ctx, userID, dbID); err != nil {
		return nil, err
	}
	if s.execTimeout <= 0 {
		return nil, fmt.Errorf("migration execution timeout must be positive")
	}
	m, err := s.repo.Get(ctx, migrationID)
	if err != nil {
		return nil, err
	}
	if m.DatabaseID != dbID {
		return nil, apperr.ErrNotFound
	}
	sql, claimed, err := s.repo.Claim(ctx, migrationID, userID)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, fmt.Errorf("%w: pastikan semua versi sebelumnya sukses dan tidak ada migration yang berjalan", apperr.ErrNotExecutable)
	}

	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.execTimeout)
	defer cancel()
	start := time.Now()
	runErr := s.run(runCtx, dbID, sql)
	durationMs := int(time.Since(start).Milliseconds())

	status, message := StatusSuccess, ""
	if runErr != nil {
		status, message = StatusFailed, describeError(runErr)
	}
	if err := s.repo.Finish(context.WithoutCancel(ctx), migrationID, status, message, durationMs); err != nil {
		slog.Error("finish migration failed", "migration_id", migrationID, "status", status, "err", err)
		return nil, err
	}
	return s.repo.Get(ctx, migrationID)
}

func (s *Service) run(ctx context.Context, databaseID int64, sql string) error {
	pool, err := s.pools.Pool(ctx, databaseID)
	if err != nil {
		return err
	}
	return s.runner.Run(ctx, pool, sql)
}

func (s *Service) Explain(ctx context.Context, userID, dbID int64, statement string) (string, error) {
	if err := s.requireEdit(ctx, userID, dbID); err != nil {
		return "", err
	}
	if strings.TrimSpace(statement) == "" || len(statement) > maxSQLBytes || strings.Contains(statement, ";") {
		return "", fmt.Errorf("%w: EXPLAIN hanya menerima satu statement", apperr.ErrInvalidInput)
	}
	pool, err := s.pools.Pool(ctx, dbID)
	if err != nil {
		return "", err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return "", dbtarget.ClassifyError(err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	rows, err := tx.Query(ctx, "EXPLAIN "+statement, pgx.QueryExecModeSimpleProtocol)
	if err != nil {
		return "", dbtarget.ClassifyError(err)
	}
	defer rows.Close()
	var result strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", err
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	return result.String(), rows.Err()
}

func describeError(err error) string {
	var pgErr *pgconn.PgError
	var message string
	switch {
	case errors.As(err, &pgErr):
		message = fmt.Sprintf("%s (SQLSTATE %s)", pgErr.Message, pgErr.Code)
		if pgErr.Detail != "" {
			message += ": " + pgErr.Detail
		}
	case errors.Is(err, context.DeadlineExceeded):
		message = "eksekusi melewati batas waktu"
	case errors.Is(err, apperr.ErrConnectionFailed):
		message = "tidak dapat terhubung ke database target"
	default:
		message = "eksekusi gagal"
		slog.Error("migration run error", "err", err)
	}
	if len(message) > 2000 {
		message = message[:2000]
	}
	return message
}

package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type SQLEvent struct {
	Database    string 
	Query       string
	Params      []any
	Duration    time.Duration
	RowCount    int64 
	Err         error
	ReadOnly    bool
	MigrationID int64
	JobID       int64
	ExecutionID int64
	Attempt     int
	Batch       int
}

func SQLState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func (e SQLEvent) attrs(phase string) []any {
	a := []any{
		"event", "sql." + phase,
		"database", e.Database,
		"query", RedactSQL(e.Query),
		"read_only", e.ReadOnly,
	}
	if p := RedactParams(e.Query, e.Params); p != nil {
		a = append(a, "params", p)
	}
	if e.MigrationID > 0 {
		a = append(a, "migration_id", e.MigrationID)
	}
	if e.JobID > 0 {
		a = append(a, "job_id", e.JobID)
	}
	if e.ExecutionID > 0 {
		a = append(a, "execution_id", e.ExecutionID)
	}
	if e.Attempt > 0 {
		a = append(a, "attempt", e.Attempt)
	}
	if e.Batch > 0 {
		a = append(a, "batch", e.Batch)
	}
	return a
}

func SQLStart(ctx context.Context, e SQLEvent) {
	slog.InfoContext(ctx, "sql dimulai", append(e.attrs("start"), "status", "started")...)
}

func SQLResult(ctx context.Context, e SQLEvent) {
	a := e.attrs("result")
	a = append(a, "duration_ms", e.Duration.Milliseconds())
	if e.RowCount >= 0 {
		a = append(a, "rows", e.RowCount)
	}
	if e.Err != nil {
		a = append(a, "status", "failed", "error", e.Err, "sqlstate", SQLState(e.Err))
		slog.ErrorContext(ctx, "sql gagal", a...)
		return
	}
	slog.InfoContext(ctx, "sql selesai", append(a, "status", "success")...)
}

func SQLCommit(ctx context.Context, e SQLEvent) {
	a := append(e.attrs("commit"), "duration_ms", e.Duration.Milliseconds())
	if e.Err != nil {
		a = append(a, "status", "failed", "error", e.Err, "sqlstate", SQLState(e.Err))
		slog.ErrorContext(ctx, "commit gagal", a...)
		return
	}
	slog.InfoContext(ctx, "commit berhasil", append(a, "status", "success")...)
}

func SQLRollback(ctx context.Context, e SQLEvent) {
	a := append(e.attrs("rollback"), "duration_ms", e.Duration.Milliseconds())
	if e.Err != nil {
		a = append(a, "status", "failed", "error", e.Err, "sqlstate", SQLState(e.Err))
		slog.WarnContext(ctx, "rollback transaksi karena error", a...)
		return
	}
	slog.InfoContext(ctx, "rollback transaksi", append(a, "status", "rolled_back")...)
}

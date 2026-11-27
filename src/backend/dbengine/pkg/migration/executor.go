package migration

import (
	"context"
	"fmt"
	"time"

	"backend/dbengine/pkg/dbtarget"
	"backend/dbengine/pkg/telemetry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SQLRunner interface {
	Run(ctx context.Context, pool *pgxpool.Pool, sql string) error
}

type Executor struct{ StatementTimeout time.Duration }

func (e *Executor) Run(ctx context.Context, pool *pgxpool.Pool, sql string) (runErr error) {
	if e.StatementTimeout <= 0 {
		return fmt.Errorf("migration statement timeout must be positive")
	}
	ctx, span := telemetry.StartSpan(ctx, "db.query")
	defer func() { telemetry.EndSpan(span, runErr) }()

	timeoutMs := int64(e.StatementTimeout / time.Millisecond)
	if e.StatementTimeout%time.Millisecond != 0 {
		timeoutMs++
	}
	if timeoutMs < 1 {
		timeoutMs = 1
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return dbtarget.ClassifyError(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs)); err != nil {
		return dbtarget.ClassifyError(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
		return dbtarget.ClassifyError(err)
	}

	ev := telemetry.SQLEvent{
		Database: "target",
		Query:    sql,
		RowCount: -1,
	}
	telemetry.SQLStart(ctx, ev)
	t0 := time.Now()
	tag, execErr := tx.Exec(ctx, sql, pgx.QueryExecModeSimpleProtocol)
	ev.Duration = time.Since(t0)
	if execErr != nil {
		ev.Err = execErr
		telemetry.SQLResult(ctx, ev)
		telemetry.SQLRollback(ctx, ev)
		return execErr
	}
	ev.RowCount = tag.RowsAffected()
	telemetry.SQLResult(ctx, ev)

	commitStart := time.Now()
	commitErr := tx.Commit(ctx)
	ev.Duration = time.Since(commitStart)
	ev.Err = commitErr
	telemetry.SQLCommit(ctx, ev)
	return commitErr
}

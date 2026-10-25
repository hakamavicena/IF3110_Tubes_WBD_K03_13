package migration

import (
	"context"
	"fmt"
	"time"

	"backend/dbengine/pkg/dbtarget"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SQLRunner interface {
	Run(ctx context.Context, pool *pgxpool.Pool, sql string) error
}

type Executor struct{ StatementTimeout time.Duration }

func (e *Executor) Run(ctx context.Context, pool *pgxpool.Pool, sql string) error {
	if e.StatementTimeout <= 0 {
		return fmt.Errorf("migration statement timeout must be positive")
	}
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
	if _, err := tx.Exec(ctx, sql, pgx.QueryExecModeSimpleProtocol); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

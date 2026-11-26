package migration

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"backend/dbengine/pkg/apperr"
	"backend/dbengine/pkg/dbtarget"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BatchProgress struct {
	CommittedBatches int
	CommittedRows    int64
	LastCursor       string
}

type BatchProgressCallback func(ctx context.Context, progress BatchProgress) error

var (
	reTargetTable = regexp.MustCompile(`(?i)^\s*(?:UPDATE|DELETE\s+FROM)\s+([a-zA-Z_][a-zA-Z0-9_.]*)\b`)
	reWhereClause = regexp.MustCompile(`(?i)\bWHERE\b`)
	reIdentPart   = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

func EnsureCheckpointTable(ctx context.Context, targetPool *pgxpool.Pool) error {
	_, err := targetPool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS _platform_batch_checkpoints (
			job_id        BIGINT NOT NULL,
			batch_number  INT NOT NULL,
			start_cursor  TEXT NOT NULL,
			end_cursor    TEXT NOT NULL,
			rows_affected BIGINT NOT NULL,
			undo_payload  JSONB,
			committed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			reverted_at   TIMESTAMPTZ,
			PRIMARY KEY (job_id, batch_number)
		)`)
	if err != nil {
		return dbtarget.ClassifyError(err)
	}
	return nil
}

func ListActiveCheckpoints(ctx context.Context, targetPool *pgxpool.Pool, checkpointJobID int64) ([]BatchCheckpoint, error) {
	rows, err := targetPool.Query(ctx, `
		SELECT job_id, batch_number, start_cursor, end_cursor, rows_affected,
		       COALESCE(undo_payload::text, ''), committed_at, reverted_at
		FROM _platform_batch_checkpoints
		WHERE job_id = $1 AND reverted_at IS NULL
		ORDER BY batch_number ASC`,
		checkpointJobID,
	)
	if err != nil {
		return nil, dbtarget.ClassifyError(err)
	}
	defer rows.Close()

	var out []BatchCheckpoint
	for rows.Next() {
		var cp BatchCheckpoint
		if err := rows.Scan(
			&cp.JobID, &cp.BatchNumber, &cp.StartCursor, &cp.EndCursor,
			&cp.RowsAffected, &cp.UndoPayload, &cp.CommittedAt, &cp.RevertedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, cp)
	}
	return out, rows.Err()
}

func (e *Executor) RunBatch(
	ctx context.Context,
	targetPool *pgxpool.Pool,
	checkpointJobID int64,
	rawSQL string,
	batchColumn string,
	batchSize int,
	onProgress BatchProgressCallback,
) (BatchProgress, Status, error) {
	var progress BatchProgress

	if e.StatementTimeout <= 0 {
		return progress, StatusFailed, fmt.Errorf("migration statement timeout must be positive")
	}
	if batchSize <= 0 {
		return progress, StatusFailed, fmt.Errorf("%w: batch_size harus lebih besar dari 0", apperr.ErrInvalidInput)
	}
	batchColumn = strings.TrimSpace(batchColumn)
	if !reIdentPart.MatchString(batchColumn) {
		return progress, StatusFailed, fmt.Errorf("%w: batch_column %q tidak valid", apperr.ErrInvalidInput, batchColumn)
	}

	tableName, err := extractTargetTable(rawSQL)
	if err != nil {
		return progress, StatusFailed, err
	}
	quotedTable, err := quoteQualifiedIdentifier(tableName)
	if err != nil {
		return progress, StatusFailed, err
	}
	quotedCol := pgx.Identifier{batchColumn}.Sanitize()
	scopedSQL := buildBatchScopedSQL(rawSQL, quotedCol)

	if err := EnsureCheckpointTable(ctx, targetPool); err != nil {
		return progress, StatusFailed, err
	}

	existing, err := ListActiveCheckpoints(ctx, targetPool, checkpointJobID)
	if err != nil {
		return progress, StatusFailed, err
	}
	existingByBatch := make(map[int]BatchCheckpoint, len(existing))
	for _, cp := range existing {
		existingByBatch[cp.BatchNumber] = cp
	}

	timeoutMs := e.timeoutMilliseconds()

	for batchNum := 1; ; batchNum++ {
		if cp, ok := existingByBatch[batchNum]; ok {
			progress.CommittedBatches = batchNum
			progress.CommittedRows += cp.RowsAffected
			progress.LastCursor = cp.EndCursor
			if onProgress != nil {
				_ = onProgress(ctx, progress)
			}
			continue
		}

		startCursor, endCursor, hasWindow, err := findNextBatchWindow(ctx, targetPool, quotedTable, quotedCol, progress.LastCursor, batchSize)
		if err != nil {
			return progress, determineBatchFailureStatus(progress.CommittedBatches), err
		}
		if !hasWindow {
			break
		}

		rowsAffected, err := executeSingleBatchTx(
			ctx, targetPool, timeoutMs,
			checkpointJobID, batchNum,
			scopedSQL, startCursor, endCursor,
		)
		if err != nil {
			return progress, determineBatchFailureStatus(progress.CommittedBatches), err
		}

		progress.CommittedBatches = batchNum
		progress.CommittedRows += rowsAffected
		progress.LastCursor = endCursor
		if onProgress != nil {
			_ = onProgress(ctx, progress)
		}
	}

	return progress, StatusSuccess, nil
}

func (e *Executor) RunBatchCancel(
	ctx context.Context,
	targetPool *pgxpool.Pool,
	checkpointJobID int64,
	rawSQLDown string,
	batchColumn string,
	onProgress BatchProgressCallback,
) (BatchProgress, Status, error) {
	var progress BatchProgress

	if e.StatementTimeout <= 0 {
		return progress, StatusFailed, fmt.Errorf("migration statement timeout must be positive")
	}
	batchColumn = strings.TrimSpace(batchColumn)
	if !reIdentPart.MatchString(batchColumn) {
		return progress, StatusFailed, fmt.Errorf("%w: batch_column %q tidak valid", apperr.ErrInvalidInput, batchColumn)
	}
	if strings.TrimSpace(rawSQLDown) == "" {
		return progress, StatusFailed, fmt.Errorf("%w: sql_down wajib diisi untuk membatalkan batch", apperr.ErrInvalidInput)
	}

	if err := EnsureCheckpointTable(ctx, targetPool); err != nil {
		return progress, StatusFailed, err
	}

	activeCheckpoints, err := ListActiveCheckpoints(ctx, targetPool, checkpointJobID)
	if err != nil {
		return progress, StatusFailed, err
	}
	if len(activeCheckpoints) == 0 {
		return progress, StatusSuccess, nil
	}

	quotedCol := pgx.Identifier{batchColumn}.Sanitize()
	scopedSQLDown := buildBatchScopedSQL(rawSQLDown, quotedCol)
	timeoutMs := e.timeoutMilliseconds()

	revertedBatches := 0
	for i := len(activeCheckpoints) - 1; i >= 0; i-- {
		cp := activeCheckpoints[i]

		revertedRows, err := revertSingleBatchTx(
			ctx, targetPool, timeoutMs,
			checkpointJobID, cp.BatchNumber,
			scopedSQLDown, cp.StartCursor, cp.EndCursor,
		)
		if err != nil {
			if revertedBatches > 0 {
				return progress, StatusPartiallyReverted, err
			}
			return progress, StatusPartial, err
		}

		revertedBatches++
		progress.CommittedBatches = len(activeCheckpoints) - revertedBatches
		progress.CommittedRows += revertedRows
		progress.LastCursor = cp.StartCursor
		if onProgress != nil {
			_ = onProgress(ctx, progress)
		}
	}

	return progress, StatusSuccess, nil
}

func determineBatchFailureStatus(committedBatches int) Status {
	if committedBatches > 0 {
		return StatusPartial
	}
	return StatusFailed
}

func (e *Executor) timeoutMilliseconds() int64 {
	timeoutMs := int64(e.StatementTimeout / time.Millisecond)
	if e.StatementTimeout%time.Millisecond != 0 {
		timeoutMs++
	}
	if timeoutMs < 1 {
		timeoutMs = 1
	}
	return timeoutMs
}

func findNextBatchWindow(
	ctx context.Context,
	targetPool *pgxpool.Pool,
	quotedTable string,
	quotedCol string,
	lastCursor string,
	batchSize int,
) (startCursor string, endCursor string, hasWindow bool, err error) {
	var rows pgx.Rows
	if lastCursor == "" {
		query := fmt.Sprintf(
			`SELECT (%s)::text FROM %s ORDER BY %s ASC LIMIT $1`,
			quotedCol, quotedTable, quotedCol,
		)
		rows, err = targetPool.Query(ctx, query, batchSize)
	} else {
		query := fmt.Sprintf(
			`SELECT (%s)::text FROM %s WHERE %s > $1 ORDER BY %s ASC LIMIT $2`,
			quotedCol, quotedTable, quotedCol, quotedCol,
		)
		rows, err = targetPool.Query(ctx, query, parseCursorParam(lastCursor), batchSize)
	}
	if err != nil {
		return "", "", false, dbtarget.ClassifyError(err)
	}
	defer rows.Close()

	first := true
	for rows.Next() {
		var cur string
		if err := rows.Scan(&cur); err != nil {
			return "", "", false, err
		}
		if first {
			startCursor = cur
			first = false
		}
		endCursor = cur
		hasWindow = true
	}
	if err := rows.Err(); err != nil {
		return "", "", false, err
	}
	return startCursor, endCursor, hasWindow, nil
}

func executeSingleBatchTx(
	ctx context.Context,
	targetPool *pgxpool.Pool,
	timeoutMs int64,
	checkpointJobID int64,
	batchNum int,
	scopedSQL string,
	startCursor string,
	endCursor string,
) (int64, error) {
	tx, err := targetPool.Begin(ctx)
	if err != nil {
		return 0, dbtarget.ClassifyError(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs)); err != nil {
		return 0, dbtarget.ClassifyError(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
		return 0, dbtarget.ClassifyError(err)
	}

	var existingRows int64
	err = tx.QueryRow(ctx, `
		SELECT rows_affected
		FROM _platform_batch_checkpoints
		WHERE job_id = $1 AND batch_number = $2 AND reverted_at IS NULL`,
		checkpointJobID, batchNum,
	).Scan(&existingRows)
	if err == nil {
		return existingRows, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}

	tag, err := tx.Exec(ctx, scopedSQL, parseCursorParam(startCursor), parseCursorParam(endCursor))
	if err != nil {
		return 0, err
	}
	rowsAffected := tag.RowsAffected()

	_, err = tx.Exec(ctx, `
		INSERT INTO _platform_batch_checkpoints (
			job_id, batch_number, start_cursor, end_cursor, rows_affected, committed_at, reverted_at
		) VALUES ($1, $2, $3, $4, $5, now(), NULL)
		ON CONFLICT (job_id, batch_number) DO UPDATE
		SET start_cursor  = EXCLUDED.start_cursor,
		    end_cursor    = EXCLUDED.end_cursor,
		    rows_affected = EXCLUDED.rows_affected,
		    committed_at  = now(),
		    reverted_at   = NULL`,
		checkpointJobID, batchNum, startCursor, endCursor, rowsAffected,
	)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return rowsAffected, nil
}

func revertSingleBatchTx(
	ctx context.Context,
	targetPool *pgxpool.Pool,
	timeoutMs int64,
	checkpointJobID int64,
	batchNum int,
	scopedSQLDown string,
	startCursor string,
	endCursor string,
) (int64, error) {
	tx, err := targetPool.Begin(ctx)
	if err != nil {
		return 0, dbtarget.ClassifyError(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs)); err != nil {
		return 0, dbtarget.ClassifyError(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
		return 0, dbtarget.ClassifyError(err)
	}

	tag, err := tx.Exec(ctx, scopedSQLDown, parseCursorParam(startCursor), parseCursorParam(endCursor))
	if err != nil {
		return 0, err
	}

	updateTag, err := tx.Exec(ctx, `
		UPDATE _platform_batch_checkpoints
		SET reverted_at = now()
		WHERE job_id = $1 AND batch_number = $2 AND reverted_at IS NULL`,
		checkpointJobID, batchNum,
	)
	if err != nil {
		return 0, err
	}
	if updateTag.RowsAffected() == 0 {
		return 0, fmt.Errorf("checkpoint job %d batch %d sudah di-revert sebelumnya", checkpointJobID, batchNum)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func extractTargetTable(rawSQL string) (string, error) {
	trimmed := strings.TrimSpace(rawSQL)
	matches := reTargetTable.FindStringSubmatch(trimmed)
	if len(matches) < 2 {
		return "", fmt.Errorf("%w: mode batch membutuhkan statement UPDATE <tabel> atau DELETE FROM <tabel>", apperr.ErrInvalidInput)
	}
	return matches[1], nil
}

func quoteQualifiedIdentifier(ident string) (string, error) {
	parts := strings.Split(ident, ".")
	if len(parts) == 0 || len(parts) > 2 {
		return "", fmt.Errorf("%w: nama tabel %q tidak valid", apperr.ErrInvalidInput, ident)
	}
	for _, p := range parts {
		if !reIdentPart.MatchString(p) {
			return "", fmt.Errorf("%w: identifier %q tidak valid", apperr.ErrInvalidInput, p)
		}
	}
	return pgx.Identifier(parts).Sanitize(), nil
}

func buildBatchScopedSQL(rawSQL string, quotedCol string) string {
	cleaned := strings.TrimSpace(rawSQL)
	cleaned = strings.TrimRight(cleaned, "; \t\r\n")

	if strings.Contains(cleaned, "{{start_cursor}}") || strings.Contains(cleaned, "{{end_cursor}}") {
		cleaned = strings.ReplaceAll(cleaned, "{{start_cursor}}", "$1")
		cleaned = strings.ReplaceAll(cleaned, "{{end_cursor}}", "$2")
		return cleaned
	}
	if strings.Contains(cleaned, "$1") && strings.Contains(cleaned, "$2") {
		return cleaned
	}

	loc := reWhereClause.FindStringIndex(cleaned)
	if loc == nil {
		return fmt.Sprintf("%s WHERE %s >= $1 AND %s <= $2", cleaned, quotedCol, quotedCol)
	}

	prefix := strings.TrimSpace(cleaned[:loc[0]])
	condition := strings.TrimSpace(cleaned[loc[1]:])
	return fmt.Sprintf("%s WHERE (%s) AND %s >= $1 AND %s <= $2", prefix, condition, quotedCol, quotedCol)
}

func parseCursorParam(cursor string) any {
	if n, err := strconv.ParseInt(cursor, 10, 64); err == nil {
		return n
	}
	return cursor
}

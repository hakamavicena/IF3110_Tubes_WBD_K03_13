package migration

import (
	"context"
	"errors"
	"fmt"

	"github.com/hakamavicena/if3110-dbengine/pkg/apperr"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	Create(ctx context.Context, migration *Migration) (*Migration, error)
	Get(ctx context.Context, id int64) (*Migration, error)
	List(ctx context.Context, databaseID int64) ([]Migration, error)
	UpdateDraft(ctx context.Context, id, databaseID int64, title, sql string) error
	Claim(ctx context.Context, id, userID int64) (sql string, ok bool, err error)
	Finish(ctx context.Context, id int64, status Status, errMsg string, durationMs int) error
	RecoverStuck(ctx context.Context) (int64, error)
}

type PGRepository struct{ db *pgxpool.Pool }

func NewPGRepository(db *pgxpool.Pool) *PGRepository { return &PGRepository{db: db} }

const selectMigration = `
	SELECT m.id, m.database_id, m.version, m.title, m.sql, m.status,
	       COALESCE(m.error_message,''), m.created_by, cu.username, m.created_at,
	       m.executed_by, eu.username, m.executed_at, m.duration_ms
	FROM migrations m
	JOIN users cu ON cu.id = m.created_by
	LEFT JOIN users eu ON eu.id = m.executed_by`

func scanMigration(row pgx.Row) (*Migration, error) {
	var m Migration
	var status string
	err := row.Scan(&m.ID, &m.DatabaseID, &m.Version, &m.Title, &m.SQL, &status,
		&m.ErrorMessage, &m.CreatedBy, &m.CreatedByName, &m.CreatedAt,
		&m.ExecutedBy, &m.ExecutedByName, &m.ExecutedAt, &m.DurationMs)
	if err != nil {
		return nil, err
	}
	m.Status = Status(status)
	return &m, nil
}

func (r *PGRepository) Create(ctx context.Context, m *Migration) (*Migration, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", m.DatabaseID); err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO migrations (database_id, version, title, sql, status, created_by)
		VALUES ($1, (SELECT COALESCE(MAX(version),0)+1 FROM migrations WHERE database_id=$1),
		        $2, $3, 'draft', $4)
		RETURNING id`, m.DatabaseID, m.Title, m.SQL, m.CreatedBy).Scan(&m.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.Get(ctx, m.ID)
}

func (r *PGRepository) Get(ctx context.Context, id int64) (*Migration, error) {
	m, err := scanMigration(r.db.QueryRow(ctx, selectMigration+" WHERE m.id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	return m, err
}

func (r *PGRepository) List(ctx context.Context, databaseID int64) ([]Migration, error) {
	rows, err := r.db.Query(ctx, selectMigration+" WHERE m.database_id=$1 ORDER BY m.version DESC", databaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Migration
	for rows.Next() {
		m, err := scanMigration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *PGRepository) UpdateDraft(ctx context.Context, id, databaseID int64, title, sql string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE migrations SET title=$3, sql=$4, status='draft', error_message=NULL
		WHERE id=$1 AND database_id=$2 AND status IN ('draft','failed')`, id, databaseID, title, sql)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: migration tidak ditemukan atau sudah final/berjalan", apperr.ErrNotExecutable)
	}
	return nil
}

// Claim serializes by database so concurrent claims cannot start different versions together.
func (r *PGRepository) Claim(ctx context.Context, id, userID int64) (string, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)
	var databaseID int64
	if err := tx.QueryRow(ctx, "SELECT database_id FROM migrations WHERE id=$1", id).Scan(&databaseID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", databaseID); err != nil {
		return "", false, err
	}
	var sql string
	err = tx.QueryRow(ctx, `
		UPDATE migrations m
		SET status='running', executed_by=$2, executed_at=now(), error_message=NULL, duration_ms=NULL
		WHERE m.id=$1 AND m.status IN ('draft','failed')
		  AND NOT EXISTS (
		      SELECT 1 FROM migrations p
		      WHERE p.database_id=m.database_id
		        AND ((p.version<m.version AND p.status<>'success') OR p.status='running'))
		RETURNING m.sql`, id, userID).Scan(&sql)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return sql, true, nil
}

func (r *PGRepository) Finish(ctx context.Context, id int64, status Status, errMsg string, durationMs int) error {
	if status != StatusSuccess && status != StatusFailed {
		return fmt.Errorf("invalid terminal migration status %q", status)
	}
	if durationMs < 0 {
		return fmt.Errorf("migration duration cannot be negative")
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE migrations SET status=$2, error_message=NULLIF($3,''), duration_ms=$4
		WHERE id=$1 AND status='running'`, id, string(status), errMsg, durationMs)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("migration %d is not running", id)
	}
	return nil
}

func (r *PGRepository) RecoverStuck(ctx context.Context) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE migrations SET status='failed', error_message='interrupted: server restarted during execution'
		WHERE status='running'`)
	return tag.RowsAffected(), err
}

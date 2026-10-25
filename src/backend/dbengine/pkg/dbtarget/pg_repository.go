package dbtarget

import (
	"context"
	"errors"

	"github.com/hakamavicena/if3110-dbengine/pkg/apperr"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGTargetRepository struct{ db *pgxpool.Pool }

func NewPGTargetRepository(db *pgxpool.Pool) *PGTargetRepository {
	return &PGTargetRepository{db: db}
}

func (r *PGTargetRepository) GetTarget(ctx context.Context, id int64) (*Target, error) {
	var target Target
	err := r.db.QueryRow(ctx, `
		SELECT id, name, host, port, database_name, username, password_enc, ssl_mode
		FROM database_targets
		WHERE id=$1`, id).Scan(
		&target.ID, &target.Name, &target.Host, &target.Port,
		&target.DatabaseName, &target.Username, &target.PasswordEnc, &target.SSLMode,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &target, nil
}

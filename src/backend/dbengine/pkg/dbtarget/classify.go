package dbtarget

import (
	"context"
	"errors"
	"fmt"

	"github.com/hakamavicena/if3110-dbengine/pkg/apperr"
	"github.com/jackc/pgx/v5/pgconn"
)

func ClassifyError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr),
		errors.Is(err, apperr.ErrNotFound),
		errors.Is(err, apperr.ErrForbidden),
		errors.Is(err, apperr.ErrInvalidInput),
		errors.Is(err, apperr.ErrConnectionFailed),
		errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return fmt.Errorf("%w: %v", apperr.ErrConnectionFailed, err)
	}
}

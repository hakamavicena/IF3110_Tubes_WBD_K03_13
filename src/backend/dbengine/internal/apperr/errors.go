package apperr

import "errors"

var (
	ErrForbidden        = errors.New("forbidden")
	ErrNotFound         = errors.New("not found")
	ErrInvalidInput     = errors.New("invalid input")
	ErrConnectionFailed = errors.New("target database connection failed")
	ErrNotExecutable    = errors.New("migration cannot be executed in its current state")
)

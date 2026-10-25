package ports

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Level int

const (
	LevelNone Level = iota
	LevelView
	LevelEdit
)

type DatabaseAccess struct {
	DatabaseID int64
	Name       string
	Level      Level
}

type Authorizer interface {
	CanView(ctx context.Context, userID, databaseID int64) (bool, error)
	CanEdit(ctx context.Context, userID, databaseID int64) (bool, error)
	AccessibleDatabases(ctx context.Context, userID int64) ([]DatabaseAccess, error)
}

type PoolProvider interface {
	Pool(ctx context.Context, databaseID int64) (*pgxpool.Pool, error)
}

type DenyAll struct{}

func (DenyAll) CanView(context.Context, int64, int64) (bool, error) { return false, nil }
func (DenyAll) CanEdit(context.Context, int64, int64) (bool, error) { return false, nil }
func (DenyAll) AccessibleDatabases(context.Context, int64) ([]DatabaseAccess, error) {
	return []DatabaseAccess{}, nil
}

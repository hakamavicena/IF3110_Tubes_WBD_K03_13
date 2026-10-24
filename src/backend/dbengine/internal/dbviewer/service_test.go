package dbviewer

import (
	"context"
	"errors"
	"testing"

	"github.com/hakamavicena/if3110-dbengine/internal/apperr"
	"github.com/hakamavicena/if3110-dbengine/internal/ports"
	"github.com/jackc/pgx/v5/pgxpool"
)

type deniedViewerAuth struct{}

func (deniedViewerAuth) CanView(context.Context, int64, int64) (bool, error) { return false, nil }
func (deniedViewerAuth) CanEdit(context.Context, int64, int64) (bool, error) { return false, nil }
func (deniedViewerAuth) AccessibleDatabases(context.Context, int64) ([]ports.DatabaseAccess, error) {
	return nil, nil
}

type unusedViewerPools struct{}

func (unusedViewerPools) Pool(context.Context, int64) (*pgxpool.Pool, error) {
	panic("pool lookup must not happen before authorization")
}

func TestListSchemasChecksAuthorizationBeforePool(t *testing.T) {
	service := NewService(deniedViewerAuth{}, unusedViewerPools{})
	_, err := service.ListSchemas(context.Background(), 1, 1)
	if !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestNormalizePage(t *testing.T) {
	tests := []struct {
		name       string
		page, size int
		wantPage   int
		wantSize   int
	}{
		{"defaults", 0, 0, 1, defaultPageSize},
		{"caps size", 2, 9999, 2, maxPageSize},
		{"caps offset overflow", int(^uint(0) >> 1), 25, int(^uint(0)>>1) / 25, 25},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			page, size := normalizePage(test.page, test.size)
			if page != test.wantPage || size != test.wantSize {
				t.Fatalf("normalizePage() = (%d, %d), want (%d, %d)", page, size, test.wantPage, test.wantSize)
			}
		})
	}
}

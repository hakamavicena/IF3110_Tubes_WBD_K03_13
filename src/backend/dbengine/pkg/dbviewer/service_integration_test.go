//go:build integration

package dbviewer

import (
	"context"
	"errors"
	"testing"

	"github.com/hakamavicena/if3110-dbengine/pkg/apperr"
	"github.com/hakamavicena/if3110-dbengine/pkg/ports"
	"github.com/hakamavicena/if3110-dbengine/pkg/testpg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type integrationAuthz struct{ allowed bool }

func (a integrationAuthz) CanView(context.Context, int64, int64) (bool, error) {
	return a.allowed, nil
}
func (integrationAuthz) CanEdit(context.Context, int64, int64) (bool, error) {
	return false, nil
}
func (integrationAuthz) AccessibleDatabases(context.Context, int64) ([]ports.DatabaseAccess, error) {
	return nil, nil
}

type integrationPools struct{ pool *pgxpool.Pool }

func (p integrationPools) Pool(context.Context, int64) (*pgxpool.Pool, error) {
	return p.pool, nil
}

func TestServiceCatalogAndPagedData(t *testing.T) {
	pool := testpg.New(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		CREATE SCHEMA "weird""schema";
		CREATE TABLE "weird""schema".parent (id integer PRIMARY KEY);
		CREATE TABLE "weird""schema"."weird""table" (
			id integer PRIMARY KEY,
			parent_id integer REFERENCES "weird""schema".parent(id),
			value text UNIQUE,
			score integer CHECK (score > 0)
		);
		INSERT INTO "weird""schema".parent VALUES (1), (2), (3);
		INSERT INTO "weird""schema"."weird""table" VALUES
			(1, 1, 'one', 1), (2, 2, NULL, 2), (3, 3, 'three', 3);`, pgx.QueryExecModeSimpleProtocol)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(integrationAuthz{allowed: true}, integrationPools{pool: pool})
	schemas, err := service.ListSchemas(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(schemas, `weird"schema`) {
		t.Fatalf("schema with quote missing: %v", schemas)
	}
	tables, err := service.ListTables(ctx, 1, 1, `weird"schema`)
	if err != nil || len(tables) != 2 {
		t.Fatalf("tables=%v err=%v", tables, err)
	}
	detail, err := service.GetTableDetail(ctx, 1, 1, `weird"schema`, `weird"table`)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"PRIMARY KEY", "FOREIGN KEY", "UNIQUE", "CHECK"} {
		if !containsConstraint(detail.Constraints, expected) {
			t.Errorf("constraint %q missing: %+v", expected, detail.Constraints)
		}
	}
	data, err := service.GetTableData(ctx, 1, 1, `weird"schema`, `weird"table`, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if data.Total != 3 || data.Pages != 2 || len(data.Rows) != 2 {
		t.Fatalf("unexpected page metadata: %+v", data)
	}
	if data.Rows[1][2] != nil {
		t.Fatalf("SQL NULL should be represented by nil, got %v", *data.Rows[1][2])
	}
	lastPage, err := service.GetTableData(ctx, 1, 1, `weird"schema`, `weird"table`, 2, 2)
	if err != nil || len(lastPage.Rows) != 1 {
		t.Fatalf("last page rows=%d err=%v", len(lastPage.Rows), err)
	}
	if _, err := service.GetTableDetail(ctx, 1, 1, "public", "missing"); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestServiceAuthorizationPrecedesTargetPool(t *testing.T) {
	service := NewService(integrationAuthz{allowed: false}, integrationPools{})
	_, err := service.ListSchemas(context.Background(), 1, 1)
	if !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func containsString(items []string, expected string) bool {
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}

func containsConstraint(items []ConstraintInfo, expected string) bool {
	for _, item := range items {
		if item.Type == expected {
			return true
		}
	}
	return false
}

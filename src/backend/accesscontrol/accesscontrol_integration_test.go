//go:build integration

package accesscontrol

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"backend/dbengine/pkg/ports"
	_ "github.com/lib/pq"
)

func TestAccessControl_Postgres_Integration(t *testing.T) {
	dsn := os.Getenv("INTERNAL_DB_URL")
	if dsn == "" {
		dsn = os.Getenv("TEST_PG_ADMIN_URL")
	}
	if dsn == "" {
		t.Skip("Skipping integration test: INTERNAL_DB_URL or TEST_PG_ADMIN_URL not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("Cannot reach test database (%v), skipping integration test", err)
	}

	repo := NewPostgresRepository(db)
	service := NewAccessService(repo)
	authorizer := NewAuthorizer(service, repo)

	// Uji memanggil GetAccessibleDatabases pada user yang tidak ada (harus mengembalikan empty slice tanpa crash)
	dbs, err := authorizer.AccessibleDatabases(context.Background(), 999999)
	if err != nil {
		t.Fatalf("AccessibleDatabases query failed on live DB: %v", err)
	}
	if len(dbs) != 0 {
		t.Errorf("Expected 0 databases for non-existent user, got %d", len(dbs))
	}

	// Uji GetEffectiveAccess pada user yang tidak ada -> harus ErrNotFound
	level, err := service.GetEffectiveAccess(999999, 999999)
	if err == nil {
		t.Errorf("Expected error for non-existent user, got level %v", level)
	}
	if level != AccessNone {
		t.Errorf("Expected AccessNone for non-existent user, got %v", level)
	}

	_ = ports.LevelNone
}

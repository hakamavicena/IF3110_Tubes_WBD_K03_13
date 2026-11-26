package migration

import (
	"errors"
	"strings"
	"testing"

	"backend/dbengine/pkg/apperr"
)

func TestValidateInput(t *testing.T) {
	tests := []struct {
		name, title, sql string
		wantErr          bool
	}{
		{"valid", "add table", "CREATE TABLE a(id int);", false},
		{"empty title", " ", "SELECT 1", true},
		{"empty SQL", "title", "  ", true},
		{"commit", "title", "CREATE TABLE a(id int);\nCOMMIT;", true},
		{"rollback", "title", "ROLLBACK;", true},
		{"oversized", "title", strings.Repeat("x", maxSQLBytes+1), true},
		{"unicode title max", strings.Repeat("界", 201), "SELECT 1", true},
		{"commit same line after semicolon", "title", "CREATE TABLE a(id int); COMMIT;", true},
		{"begin after semicolon", "title", "SELECT 1; BEGIN;", true},
		{"start transaction", "title", "START TRANSACTION;", true},
		{"end", "title", "END;", true},
		{"savepoint", "title", "SAVEPOINT s1;", true},
		{"release", "title", "RELEASE SAVEPOINT s1;", true},
		{"prepare transaction", "title", "PREPARE TRANSACTION 'tx1';", true},
		{"transaction words in string", "title", "SELECT 'COMMIT; ROLLBACK';", false},
		{"transaction words in comments", "title", "-- COMMIT;\n/* ROLLBACK; */ SELECT 1;", false},
		{"transaction words in dollar quote", "title", "SELECT $$COMMIT; ROLLBACK$$;", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateInput(test.title, test.sql)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateInput() error = %v, wantErr %v", err, test.wantErr)
			}
			if err != nil && !errors.Is(err, apperr.ErrInvalidInput) {
				t.Fatalf("error is not ErrInvalidInput: %v", err)
			}
		})
	}
}

func TestDescribeErrorHidesGenericDetails(t *testing.T) {
	if got := describeError(errors.New("sensitive DSN")); got != "eksekusi gagal" {
		t.Fatalf("unexpected message: %q", got)
	}
}

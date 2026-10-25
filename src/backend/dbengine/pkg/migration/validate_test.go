package migration

import (
	"errors"
	"strings"
	"testing"

	"github.com/hakamavicena/if3110-dbengine/pkg/apperr"
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

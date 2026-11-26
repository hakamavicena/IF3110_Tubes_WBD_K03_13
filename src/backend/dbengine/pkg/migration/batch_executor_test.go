package migration

import (
	"errors"
	"strings"
	"testing"

	"backend/dbengine/pkg/apperr"
)

func TestExtractTargetTableAndBuildBatchScopedSQL(t *testing.T) {
	table, err := extractTargetTable("UPDATE public.orders SET status = 'archived' WHERE status = 'pending';")
	if err != nil {
		t.Fatalf("extractTargetTable failed: %v", err)
	}
	if table != "public.orders" {
		t.Fatalf("expected table public.orders, got %q", table)
	}

	if _, err := extractTargetTable("CREATE TABLE illegal_in_batch (id INT)"); !errors.Is(err, apperr.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for DDL in batch mode, got %v", err)
	}

	scopedWithWhere := buildBatchScopedSQL("UPDATE orders SET status = 'done' WHERE status = 'open';", `"id"`)
	if !strings.Contains(scopedWithWhere, `WHERE (status = 'open') AND "id" >= $1 AND "id" <= $2`) {
		t.Fatalf("unexpected scoped SQL with WHERE: %s", scopedWithWhere)
	}

	scopedWithoutWhere := buildBatchScopedSQL("UPDATE orders SET status = 'done'", `"id"`)
	if scopedWithoutWhere != `UPDATE orders SET status = 'done' WHERE "id" >= $1 AND "id" <= $2` {
		t.Fatalf("unexpected scoped SQL without WHERE: %s", scopedWithoutWhere)
	}
}

func TestDetermineBatchFailureStatus(t *testing.T) {
	wantWhenZeroCommitted := StatusFailed
	wantWhenThreeCommitted := StatusPartial

	if got := determineBatchFailureStatus(0); got != wantWhenZeroCommitted {
		t.Fatalf("determineBatchFailureStatus(0) = %q, want %q", got, wantWhenZeroCommitted)
	}
	if got := determineBatchFailureStatus(3); got != wantWhenThreeCommitted {
		t.Fatalf("determineBatchFailureStatus(3) = %q, want %q", got, wantWhenThreeCommitted)
	}
}

func TestValidateMigrationInputM2(t *testing.T) {
	tests := []struct {
		name    string
		input   MigrationInputM2
		wantErr bool
	}{
		{
			name: "valid standard reversible migration",
			input: MigrationInputM2{
				Title:         "Add phone column",
				SQL:           "ALTER TABLE users ADD COLUMN phone TEXT;",
				SQLDown:       "ALTER TABLE users DROP COLUMN phone;",
				ExecutionMode: ExecutionModeStandard,
			},
			wantErr: false,
		},
		{
			name: "missing sql_down when not irreversible",
			input: MigrationInputM2{
				Title:          "Add phone column",
				SQL:            "ALTER TABLE users ADD COLUMN phone TEXT;",
				SQLDown:        "   ",
				IsIrreversible: false,
				ExecutionMode:  ExecutionModeStandard,
			},
			wantErr: true,
		},
		{
			name: "missing reason when irreversible",
			input: MigrationInputM2{
				Title:              "Drop legacy column",
				SQL:                "ALTER TABLE users DROP COLUMN legacy_token;",
				IsIrreversible:     true,
				IrreversibleReason: "",
				ExecutionMode:      ExecutionModeStandard,
			},
			wantErr: true,
		},
		{
			name: "valid irreversible migration with reason",
			input: MigrationInputM2{
				Title:              "Drop legacy column",
				SQL:                "ALTER TABLE users DROP COLUMN legacy_token;",
				IsIrreversible:     true,
				IrreversibleReason: "Kolom legacy_token sudah tidak digunakan dan datanya dihapus permanen",
				ExecutionMode:      ExecutionModeStandard,
			},
			wantErr: false,
		},
		{
			name: "valid batch migration",
			input: MigrationInputM2{
				Title:         "Backfill tier in batches",
				SQL:           "UPDATE users SET tier = 'standard' WHERE tier IS NULL",
				SQLDown:       "UPDATE users SET tier = NULL WHERE tier = 'standard'",
				ExecutionMode: ExecutionModeBatch,
				BatchColumn:   "id",
				BatchSize:     500,
			},
			wantErr: false,
		},
		{
			name: "invalid batch migration with zero batch_size",
			input: MigrationInputM2{
				Title:         "Backfill tier in batches",
				SQL:           "UPDATE users SET tier = 'standard'",
				SQLDown:       "UPDATE users SET tier = NULL",
				ExecutionMode: ExecutionModeBatch,
				BatchColumn:   "id",
				BatchSize:     0,
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMigrationInputM2(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateMigrationInputM2(%q) error = %v, wantErr = %v", tc.name, err, tc.wantErr)
			}
		})
	}
}

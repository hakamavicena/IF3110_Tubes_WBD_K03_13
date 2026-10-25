package dbtarget

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"backend/dbengine/pkg/apperr"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSecretBoxEncryptDecrypt(t *testing.T) {
	box, err := NewSecretBox(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	first, err := box.Encrypt("p@ss:/#word")
	if err != nil {
		t.Fatal(err)
	}
	second, err := box.Encrypt("p@ss:/#word")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("encryption reused a nonce")
	}
	got, err := box.Decrypt(first)
	if err != nil || got != "p@ss:/#word" {
		t.Fatalf("got %q, err %v", got, err)
	}
	first[len(first)-1] ^= 1
	if _, err := box.Decrypt(first); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
}

func TestNewSecretBoxRejectsWrongKeySize(t *testing.T) {
	if _, err := NewSecretBox(make([]byte, 31)); err == nil {
		t.Fatal("expected key size error")
	}
}

func TestBuildConnStringEscapesPassword(t *testing.T) {
	conn := buildConnString(&Target{
		Host: "localhost", Port: 5432, DatabaseName: "demo",
		Username: "app", SSLMode: "disable",
	}, "p@ss:/#word")
	if strings.Contains(conn, "p@ss:/#word") || !strings.Contains(conn, "p%40ss%3A%2F%23word") {
		t.Fatalf("password was not escaped: %q", conn)
	}
}

func TestClassifyError(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "42601", Message: "syntax error"}
	if got := ClassifyError(pgErr); !errors.Is(got, pgErr) {
		t.Fatalf("postgres error was changed: %v", got)
	}
	got := ClassifyError(errors.New("connection reset"))
	if !errors.Is(got, apperr.ErrConnectionFailed) || !strings.Contains(got.Error(), "connection reset") {
		t.Fatalf("unexpected connection classification: %v", got)
	}
	if got := ClassifyError(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

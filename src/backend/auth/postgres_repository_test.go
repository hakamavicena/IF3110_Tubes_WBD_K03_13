//go:build integration

package auth

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func TestPostgresRepository_Integration(t *testing.T) {
	if err := godotenv.Load(); err != nil {
		log.Println("Tidak ada file .env, menggunakan variabel environment OS")
	}
	connStr := os.Getenv("INTERNAL_DB_URL")
	if connStr == "" {
		host := os.Getenv("DB_HOST")
		if host == "" {
			host = "localhost"
		}
		port := os.Getenv("DB_PORT")
		if port == "" {
			port = "5433"
		}
		connStr = fmt.Sprintf("user=%s password=%s host=%s port=%s dbname=%s sslmode=disable",
			os.Getenv("DB_USER"),
			os.Getenv("DB_PASSWORD"),
			host,
			port,
			os.Getenv("DB_NAME"),
		)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("Gagal koneksi ke DB Test: %v", err)
	}
	defer db.Close()

	repo := NewPostgresRepository(db)

	session := &Session{
		SessionID:          uuid.New().String(),
		UserID:             1,
		CreatedAt:          time.Now().Round(time.Microsecond),
		LastActivity:       time.Now().Round(time.Microsecond),
		AbsoluteExpiration: time.Now().Add(24 * time.Hour).Round(time.Microsecond),
	}

	if err := repo.SaveSession(session); err != nil {
		t.Fatalf("Gagal menyimpan session: %v", err)
	}

	savedSession, err := repo.GetSession(session.SessionID)
	if err != nil {
		t.Fatalf("Gagal mengambil session: %v", err)
	}
	if savedSession.SessionID != session.SessionID {
		t.Errorf("ID Session tidak cocok")
	}

	if err := repo.DeleteSession(session.SessionID); err != nil {
		t.Fatalf("Gagal menghapus session: %v", err)
	}
}

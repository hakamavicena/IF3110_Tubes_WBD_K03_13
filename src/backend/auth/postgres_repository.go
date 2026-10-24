package auth

import (
	"database/sql"

	_ "github.com/lib/pq"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) GetUserByUsername(username string) (*User, error) {
	user := &User{}
	query := `SELECT id, username, password_hash FROM users WHERE username = $1`
	err := r.db.QueryRow(query, username).Scan(&user.ID, &user.Username, &user.Password)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	return user, err
}

func (r *PostgresRepository) SaveSession(session *Session) error {
	query := `
		INSERT INTO sessions (session_id, user_id, created_at, last_activity, absolute_expiration)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (session_id) 
		DO UPDATE SET last_activity = EXCLUDED.last_activity`
	_, err := r.db.Exec(query, session.SessionID, session.UserID, session.CreatedAt, session.LastActivity, session.AbsoluteExpiration)
	return err
}

func (r *PostgresRepository) GetSession(sessionID string) (*Session, error) {
	session := &Session{}
	query := `SELECT session_id, user_id, created_at, last_activity, absolute_expiration FROM sessions WHERE session_id = $1`
	err := r.db.QueryRow(query, sessionID).Scan(&session.SessionID, &session.UserID, &session.CreatedAt, &session.LastActivity, &session.AbsoluteExpiration)
	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	return session, err
}

func (r *PostgresRepository) DeleteSession(sessionID string) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE session_id = $1`, sessionID)
	return err
}

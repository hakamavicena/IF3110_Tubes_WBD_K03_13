package auth

import (
	"database/sql"
	"time"

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
	query := `SELECT id, username, password_hash, is_admin, is_active FROM users WHERE username = $1`
	err := r.db.QueryRow(query, username).Scan(&user.ID, &user.Username, &user.Password, &user.IsAdmin, &user.IsActive)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	return user, err
}

func (r *PostgresRepository) GetUserByID(id int) (*User, error) {
	user := &User{}
	query := `SELECT id, username, password_hash, is_admin, is_active FROM users WHERE id = $1`
	err := r.db.QueryRow(query, id).Scan(&user.ID, &user.Username, &user.Password, &user.IsAdmin, &user.IsActive)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	return user, err
}

func (r *PostgresRepository) SaveSession(session *Session) error {
	var prevID *string
	if session.PreviousID != "" {
		prevID = &session.PreviousID
	}
	var prevUntil *time.Time
	if !session.PreviousValidUntil.IsZero() {
		prevUntil = &session.PreviousValidUntil
	}

	query := `
		INSERT INTO sessions (session_id, user_id, csrf_token, created_at, last_activity, absolute_expiration, previous_id, previous_valid_until)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (session_id) 
		DO UPDATE SET 
			csrf_token = EXCLUDED.csrf_token,
			last_activity = EXCLUDED.last_activity,
			absolute_expiration = EXCLUDED.absolute_expiration,
			previous_id = EXCLUDED.previous_id,
			previous_valid_until = EXCLUDED.previous_valid_until`
	_, err := r.db.Exec(query, session.SessionID, session.UserID, session.CSRFToken, session.CreatedAt, session.LastActivity, session.AbsoluteExpiration, prevID, prevUntil)
	return err
}

func (r *PostgresRepository) GetSession(sessionID string) (*Session, error) {
	session := &Session{}
	var prevID sql.NullString
	var prevUntil sql.NullTime

	query := `
		SELECT session_id, user_id, csrf_token, created_at, last_activity, absolute_expiration, previous_id, previous_valid_until 
		FROM sessions 
		WHERE session_id = $1 OR previous_id = $1
		LIMIT 1`
	err := r.db.QueryRow(query, sessionID).Scan(
		&session.SessionID, &session.UserID, &session.CSRFToken,
		&session.CreatedAt, &session.LastActivity, &session.AbsoluteExpiration,
		&prevID, &prevUntil,
	)
	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	if prevID.Valid {
		session.PreviousID = prevID.String
	}
	if prevUntil.Valid {
		session.PreviousValidUntil = prevUntil.Time
	}
	return session, nil
}

func (r *PostgresRepository) DeleteSession(sessionID string) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE session_id = $1 OR previous_id = $1`, sessionID)
	return err
}

func (r *PostgresRepository) DeleteUserSessions(userID int) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

package auth

import "time"

// User merepresentasikan entitas pengguna
type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Password string `json:"-"` // Hanya menyimpan password yang sudah di-hash
	IsAdmin  bool   `json:"is_admin"`
	IsActive bool   `json:"is_active"`
}

// Session merepresentasikan data sesi login yang disimpan di backend
type Session struct {
	SessionID          string    `json:"-"`
	UserID             int       `json:"user_id"`
	CSRFToken          string    `json:"-"`
	CreatedAt          time.Time `json:"created_at"`
	LastActivity       time.Time `json:"last_activity"`
	AbsoluteExpiration time.Time `json:"absolute_expiration"`
	PreviousID         string    `json:"-"`
	PreviousValidUntil time.Time `json:"-"`
}

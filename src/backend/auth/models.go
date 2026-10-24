package auth

import "time"

// User merepresentasikan entitas pengguna
type User struct {
	ID       int
	Username string
	Password string // Hanya menyimpan password yang sudah di-hash
}

// Session merepresentasikan data sesi login yang disimpan di backend
type Session struct {
	SessionID          string
	UserID             int
	CreatedAt          time.Time
	LastActivity       time.Time
	AbsoluteExpiration time.Time
}

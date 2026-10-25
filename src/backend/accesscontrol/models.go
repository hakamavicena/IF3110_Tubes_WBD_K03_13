package accesscontrol

import "errors"

// definisiin hak akses terhadap suatu database
type AccessLevel int

const (
	AccessNone AccessLevel = iota
	AccessViewOnly
	AccessViewEdit
)

type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"` // asumsi dari auth
	IsAdmin  bool   `json:"is_admin"`
}

type Group struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Database struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	DatabaseName string `json:"database_name"`
	Username     string `json:"username"`
	PasswordEnc  []byte `json:"-"`
	SSLMode      string `json:"ssl_mode"`
}

type DatabaseWithAccess struct {
	DatabaseID int64
	Name       string
	Level      AccessLevel
}

type GroupAccess struct {
	GroupID     int         `json:"group_id"`
	DatabaseID  int         `json:"database_id"`
	AccessLevel AccessLevel `json:"access_level"`
}

var (
	ErrUnauthorized = errors.New("pengguna tidak memiliki akses")
	ErrNotAdmin     = errors.New("hanya Platform Admin yang dapat melakukan aksi ini")
	ErrNotFound     = errors.New("data tidak ditemukan")
)

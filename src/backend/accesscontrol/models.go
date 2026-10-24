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
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"-"`
	DBName   string `json:"db_name"`
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

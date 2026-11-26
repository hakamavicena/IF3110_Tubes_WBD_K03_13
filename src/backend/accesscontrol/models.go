package accesscontrol

import "errors"

// definisiin hak akses terhadap suatu database
type AccessLevel int

// File ini adalah "kamus" yang mendefinisikan bentuk tipe data (struct) 
// dan error standar yang akan dipakai oleh seluruh file lain di modul ini.

// Spek meminta adanya User, Grup, Database, dan tingkat akses (View Only / View + Edit). 
// Di sinilah hal itu diterjemahkan ke kode Golang.
const (
	AccessNone AccessLevel = iota // bernilai 0
	AccessViewOnly // bernilai 1
	AccessViewEdit // bernilai 2
)

type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	IsActive bool   `json:"is_active"`
}

type GroupDatabaseGrant struct {
	DatabaseID   int         `json:"database_id"`
	DatabaseName string      `json:"database_name"`
	AccessLevel  AccessLevel `json:"access_level"`
}

type GroupDetail struct {
	Group   Group
	Members []User
	Grants  []GroupDatabaseGrant
}

type UserWithGroups struct {
	User   User
	Groups []Group
}

type Group struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	LeaderID       *int   `json:"leader_id"`
	LeaderUsername string `json:"leader_username,omitempty"`
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
	ErrInvalidInput  = errors.New("input tidak valid")
	ErrConflict      = errors.New("operasi bertentangan dengan keadaan saat ini")
	ErrUnauthorized = errors.New("pengguna tidak memiliki akses")
	ErrNotAdmin     = errors.New("hanya Platform Admin yang dapat melakukan aksi ini")
	ErrNotFound     = errors.New("data tidak ditemukan")
)

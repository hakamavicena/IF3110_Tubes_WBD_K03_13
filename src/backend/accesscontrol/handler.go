package accesscontrol

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/dbengine/pkg/dbtarget"
)

type AdminHandler struct {
	service *AccessService
	repo    AccessRepository
	manager *dbtarget.Manager
	box     *dbtarget.SecretBox
}

func NewAdminHandler(service *AccessService, repo AccessRepository, manager *dbtarget.Manager, box *dbtarget.SecretBox) *AdminHandler {
	return &AdminHandler{
		service: service,
		repo:    repo,
		manager: manager,
		box:     box,
	}
}

func (h *AdminHandler) AddUserToGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := strconv.Atoi(r.FormValue("user_id"))
	groupID, _ := strconv.Atoi(r.FormValue("group_id"))

	if err := h.repo.AddUserToGroup(userID, groupID); err != nil {
		http.Error(w, "Gagal menambahkan user ke group", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Berhasil menambahkan user ke group"))
}

func (h *AdminHandler) SetGroupAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	groupID, _ := strconv.Atoi(r.FormValue("group_id"))
	dbIDStr := r.FormValue("db_id")
	if dbIDStr == "" {
		dbIDStr = r.FormValue("database_id")
	}
	dbID, _ := strconv.Atoi(dbIDStr)
	level, _ := strconv.Atoi(r.FormValue("access_level"))

	if err := h.repo.SetGroupAccess(groupID, dbID, AccessLevel(level)); err != nil {
		log.Printf("Gagal SetGroupAccess (group %d, db %d, level %d): %v", groupID, dbID, level, err)
		http.Error(w, "Gagal mengatur akses group", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Berhasil mengatur akses group"))
}

func isValidSSLMode(mode string) bool {
	switch mode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return true
	default:
		return false
	}
}

func (h *AdminHandler) CreateDatabaseConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	host := strings.TrimSpace(r.FormValue("host"))
	portStr := strings.TrimSpace(r.FormValue("port"))
	dbName := strings.TrimSpace(r.FormValue("database_name"))
	if dbName == "" {
		dbName = strings.TrimSpace(r.FormValue("db_name"))
	}
	username := strings.TrimSpace(r.FormValue("username"))
	if username == "" {
		username = strings.TrimSpace(r.FormValue("user"))
	}
	password := r.FormValue("password")
	sslMode := strings.TrimSpace(r.FormValue("ssl_mode"))
	if sslMode == "" {
		sslMode = "prefer"
	}

	if name == "" || host == "" || dbName == "" || username == "" || password == "" {
		http.Error(w, "Field name, host, database_name, username, dan password wajib diisi", http.StatusBadRequest)
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		http.Error(w, "Port harus berupa angka antara 1 dan 65535", http.StatusBadRequest)
		return
	}

	if !isValidSSLMode(sslMode) {
		http.Error(w, "Nilai ssl_mode tidak valid", http.StatusBadRequest)
		return
	}

	target := &dbtarget.Target{
		Name:         name,
		Host:         host,
		Port:         port,
		DatabaseName: dbName,
		Username:     username,
		SSLMode:      sslMode,
	}

	// Uji koneksi sebelum menyimpan jika manager terpasang
	if h.manager != nil {
		testCtx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		if err := h.manager.TestConnection(testCtx, target, password); err != nil {
			http.Error(w, "Gagal menghubungi database target. Pastikan host, port, kredensial, dan status database benar.", http.StatusBadRequest)
			return
		}
	}

	// Enkripsi password dengan SecretBox
	var passwordEnc []byte
	if h.box != nil {
		enc, err := h.box.Encrypt(password)
		if err != nil {
			http.Error(w, "Gagal mengenkripsi password database target", http.StatusInternalServerError)
			return
		}
		passwordEnc = enc
	}

	db := &Database{
		Name:         name,
		Host:         host,
		Port:         port,
		DatabaseName: dbName,
		Username:     username,
		PasswordEnc:  passwordEnc,
		SSLMode:      sslMode,
	}

	if err := h.repo.CreateDatabase(db); err != nil {
		http.Error(w, "Gagal mendaftarkan database", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	// Jangan pernah kembalikan password atau password_enc ke response
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":            db.ID,
		"name":          db.Name,
		"host":          db.Host,
		"port":          db.Port,
		"database_name": db.DatabaseName,
		"username":      db.Username,
		"ssl_mode":      db.SSLMode,
	})
}

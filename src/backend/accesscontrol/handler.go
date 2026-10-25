package accesscontrol

import (
	"context"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"backend/auth"
	"backend/dbengine/pkg/dbtarget"
	"golang.org/x/crypto/bcrypt"
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,50}$`)

type AdminHandler struct {
	service            *AccessService
	repo               AccessRepository
	manager            *dbtarget.Manager
	box                *dbtarget.SecretBox
	templatePath       string
	template           *template.Template
	sessionInvalidator func(userID int) error
}

func NewAdminHandler(service *AccessService, repo AccessRepository, manager *dbtarget.Manager, box *dbtarget.SecretBox) *AdminHandler {
	return &AdminHandler{
		service: service,
		repo:    repo,
		manager: manager,
		box:     box,
	}
}

func (h *AdminHandler) SetTemplatePath(path string) {
	h.templatePath = path
}

func (h *AdminHandler) SetTemplate(tmpl *template.Template) {
	h.template = tmpl
}

func (h *AdminHandler) SetSessionInvalidator(f func(userID int) error) {
	h.sessionInvalidator = f
}

type AdminPageData struct {
	Users     []UserWithGroups
	Groups    []GroupDetail
	Databases []Database
	CSRFToken string
	Message   string
	Error     string
}

func (h *AdminHandler) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Ambil data users beserta grupnya
	rawUsers, err := h.repo.GetAllUsers()
	if err != nil {
		log.Printf("Gagal mengambil daftar pengguna: %v", err)
		http.Error(w, "Gagal memuat data pengguna", http.StatusInternalServerError)
		return
	}
	var usersWithGroups []UserWithGroups
	for _, u := range rawUsers {
		grps, _ := h.repo.GetGroupsByUserID(u.ID)
		usersWithGroups = append(usersWithGroups, UserWithGroups{
			User:   u,
			Groups: grps,
		})
	}

	// 2. Ambil data grup beserta anggota dan grant database
	rawGroups, err := h.repo.GetAllGroups()
	if err != nil {
		log.Printf("Gagal mengambil daftar grup: %v", err)
		http.Error(w, "Gagal memuat data grup", http.StatusInternalServerError)
		return
	}
	var groupDetails []GroupDetail
	for _, g := range rawGroups {
		members, _ := h.repo.GetGroupMembers(g.ID)
		grants, _ := h.repo.GetGroupGrants(g.ID)
		groupDetails = append(groupDetails, GroupDetail{
			Group:   g,
			Members: members,
			Grants:  grants,
		})
	}

	// 3. Ambil data koneksi database (tanpa password)
	dbs, err := h.repo.GetAllDatabases()
	if err != nil {
		log.Printf("Gagal mengambil daftar database: %v", err)
		http.Error(w, "Gagal memuat data database", http.StatusInternalServerError)
		return
	}

	csrfToken := auth.CSRFToken(r)
	msg := r.URL.Query().Get("msg")
	errMsg := r.URL.Query().Get("err")

	data := AdminPageData{
		Users:     usersWithGroups,
		Groups:    groupDetails,
		Databases: dbs,
		CSRFToken: csrfToken,
		Message:   msg,
		Error:     errMsg,
	}

	tmpl := h.template
	if tmpl == nil {
		path := h.templatePath
		if path == "" {
			path = "frontend/templates/admin.html"
		}
		t, err := template.ParseFiles(path)
		if err != nil {
			log.Printf("Gagal parse template admin (%s): %v", path, err)
			http.Error(w, "Gagal merender template admin", http.StatusInternalServerError)
			return
		}
		tmpl = t
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Gagal eksekusi template admin: %v", err)
		http.Error(w, "Gagal merender halaman admin", http.StatusInternalServerError)
	}
}

func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	isAdminVal := r.FormValue("is_admin")
	isAdmin := isAdminVal == "true" || isAdminVal == "1" || isAdminVal == "on"

	if !usernameRegex.MatchString(username) {
		h.redirectWith(w, r, "", "Username harus 3-50 karakter alfanumerik atau _ . -", http.StatusBadRequest)
		return
	}

	if len(password) < 8 {
		h.redirectWith(w, r, "", "Password minimal harus 8 karakter", http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		h.redirectWith(w, r, "", "Gagal memproses password", http.StatusInternalServerError)
		return
	}

	_, err = h.repo.CreateUserWithPassword(username, string(hash), isAdmin)
	if err != nil {
		log.Printf("Gagal membuat user %s: %v", username, err)
		h.redirectWith(w, r, "", "Gagal membuat user (username mungkin sudah digunakan)", http.StatusBadRequest)
		return
	}

	h.redirectWith(w, r, "User '"+username+"' berhasil dibuat", "", http.StatusOK)
}

func (h *AdminHandler) SetUserStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userIDStr := r.FormValue("user_id")
	targetID, err := strconv.Atoi(userIDStr)
	if err != nil || targetID <= 0 {
		h.redirectWith(w, r, "", "ID user tidak valid", http.StatusBadRequest)
		return
	}

	action := strings.TrimSpace(r.FormValue("action"))

	// Ambil ID user yang sedang login
	currentUserID, _ := auth.UserIDFromContext(r.Context())

	if action == "deactivate" {
		// Proteksi: tidak boleh menonaktifkan diri sendiri
		if int64(targetID) == currentUserID {
			h.redirectWith(w, r, "", "Admin tidak boleh menonaktifkan dirinya sendiri", http.StatusBadRequest)
			return
		}

		// Proteksi: tidak boleh menonaktifkan admin terakhir
		targetUser, err := h.repo.GetUser(targetID)
		if err != nil {
			h.redirectWith(w, r, "", "User tidak ditemukan", http.StatusNotFound)
			return
		}

		if targetUser.IsAdmin {
			activeAdmins, err := h.repo.GetActiveAdminCount()
			if err != nil {
				h.redirectWith(w, r, "", "Gagal memverifikasi jumlah admin", http.StatusInternalServerError)
				return
			}
			if activeAdmins <= 1 {
				h.redirectWith(w, r, "", "Tidak dapat menonaktifkan admin terakhir", http.StatusBadRequest)
				return
			}
		}

		if err := h.repo.SetUserActive(targetID, false); err != nil {
			log.Printf("Gagal menonaktifkan user %d: %v", targetID, err)
			h.redirectWith(w, r, "", "Gagal menonaktifkan user", http.StatusInternalServerError)
			return
		}

		// Cabut seluruh sesi user yang dinonaktifkan secara instan
		if h.sessionInvalidator != nil {
			if err := h.sessionInvalidator(targetID); err != nil {
				log.Printf("Peringatan: gagal membatalkan sesi user %d: %v", targetID, err)
			}
		}

		h.redirectWith(w, r, "User berhasil dinonaktifkan", "", http.StatusOK)
		return
	} else if action == "activate" {
		if err := h.repo.SetUserActive(targetID, true); err != nil {
			log.Printf("Gagal mengaktifkan user %d: %v", targetID, err)
			h.redirectWith(w, r, "", "Gagal mengaktifkan user", http.StatusInternalServerError)
			return
		}
		h.redirectWith(w, r, "User berhasil diaktifkan", "", http.StatusOK)
		return
	}

	h.redirectWith(w, r, "", "Aksi tidak dikenali", http.StatusBadRequest)
}

func (h *AdminHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.redirectWith(w, r, "", "Nama grup tidak boleh kosong", http.StatusBadRequest)
		return
	}

	_, err := h.repo.CreateGroup(name)
	if err != nil {
		log.Printf("Gagal membuat grup %s: %v", name, err)
		h.redirectWith(w, r, "", "Gagal membuat grup (nama mungkin sudah ada)", http.StatusBadRequest)
		return
	}

	h.redirectWith(w, r, "Grup '"+name+"' berhasil dibuat", "", http.StatusOK)
}

func (h *AdminHandler) AddUserToGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := strconv.Atoi(r.FormValue("user_id"))
	groupID, _ := strconv.Atoi(r.FormValue("group_id"))

	if userID <= 0 || groupID <= 0 {
		h.redirectWith(w, r, "", "User ID dan Group ID harus valid", http.StatusBadRequest)
		return
	}

	if err := h.repo.AddUserToGroup(userID, groupID); err != nil {
		h.redirectWith(w, r, "", "Gagal menambahkan user ke grup", http.StatusInternalServerError)
		return
	}

	h.redirectWith(w, r, "Berhasil menambahkan user ke grup", "", http.StatusOK)
}

func (h *AdminHandler) RemoveUserFromGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := strconv.Atoi(r.FormValue("user_id"))
	groupID, _ := strconv.Atoi(r.FormValue("group_id"))

	if userID <= 0 || groupID <= 0 {
		h.redirectWith(w, r, "", "User ID dan Group ID harus valid", http.StatusBadRequest)
		return
	}

	if err := h.repo.RemoveUserFromGroup(userID, groupID); err != nil {
		h.redirectWith(w, r, "", "Gagal mengeluarkan user dari grup", http.StatusInternalServerError)
		return
	}

	h.redirectWith(w, r, "Berhasil mengeluarkan user dari grup", "", http.StatusOK)
}

func (h *AdminHandler) SetGroupAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	groupID, _ := strconv.Atoi(r.FormValue("group_id"))
	dbIDStr := r.FormValue("database_id")
	if dbIDStr == "" {
		dbIDStr = r.FormValue("db_id")
	}
	dbID, _ := strconv.Atoi(dbIDStr)
	level, _ := strconv.Atoi(r.FormValue("access_level"))

	if groupID <= 0 || dbID <= 0 {
		h.redirectWith(w, r, "", "Grup dan Database harus dipilih", http.StatusBadRequest)
		return
	}

	if level == 0 {
		if err := h.repo.DeleteGroupAccess(groupID, dbID); err != nil {
			log.Printf("Gagal cabut akses (group %d, db %d): %v", groupID, dbID, err)
			h.redirectWith(w, r, "", "Gagal mencabut akses grup", http.StatusInternalServerError)
			return
		}
		h.redirectWith(w, r, "Akses grup ke database berhasil dicabut", "", http.StatusOK)
		return
	}

	if err := h.repo.SetGroupAccess(groupID, dbID, AccessLevel(level)); err != nil {
		log.Printf("Gagal SetGroupAccess (group %d, db %d, level %d): %v", groupID, dbID, level, err)
		h.redirectWith(w, r, "", "Gagal mengatur akses grup", http.StatusInternalServerError)
		return
	}

	h.redirectWith(w, r, "Hak akses grup berhasil disimpan", "", http.StatusOK)
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
		h.redirectWith(w, r, "", "Field name, host, database_name, username, dan password wajib diisi", http.StatusBadRequest)
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		h.redirectWith(w, r, "", "Port harus berupa angka antara 1 dan 65535", http.StatusBadRequest)
		return
	}

	if !isValidSSLMode(sslMode) {
		h.redirectWith(w, r, "", "Nilai ssl_mode tidak valid", http.StatusBadRequest)
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
			log.Printf("Uji koneksi gagal untuk target '%s': %v", name, err)
			h.redirectWith(w, r, "", "Gagal menghubungi database target. Pastikan host, port, kredensial, dan status database benar.", http.StatusBadRequest)
			return
		}
	}

	// Enkripsi password dengan SecretBox
	var passwordEnc []byte
	if h.box != nil {
		enc, err := h.box.Encrypt(password)
		if err != nil {
			h.redirectWith(w, r, "", "Gagal mengenkripsi password database target", http.StatusInternalServerError)
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
		log.Printf("Gagal mendaftarkan database %s: %v", name, err)
		h.redirectWith(w, r, "", "Gagal mendaftarkan database ke sistem", http.StatusInternalServerError)
		return
	}

	// Jika pemanggil mengharapkan JSON (API/curl)
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":            db.ID,
			"name":          db.Name,
			"host":          db.Host,
			"port":          db.Port,
			"database_name": db.DatabaseName,
			"username":      db.Username,
			"ssl_mode":      db.SSLMode,
		})
		return
	}

	h.redirectWith(w, r, "Koneksi database '"+name+"' berhasil didaftarkan", "", http.StatusOK)
}

func (h *AdminHandler) DeleteDatabaseConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dbIDStr := r.FormValue("database_id")
	if dbIDStr == "" {
		dbIDStr = r.FormValue("db_id")
	}
	dbID, err := strconv.Atoi(dbIDStr)
	if err != nil || dbID <= 0 {
		h.redirectWith(w, r, "", "Database ID tidak valid", http.StatusBadRequest)
		return
	}

	// Hapus koneksi dari database
	if err := h.repo.DeleteDatabase(dbID); err != nil {
		log.Printf("Gagal menghapus database %d: %v", dbID, err)
		h.redirectWith(w, r, "", "Gagal menghapus database target", http.StatusInternalServerError)
		return
	}

	// SETELAH penghapusan: panggil Manager.Invalidate(id)
	if h.manager != nil {
		h.manager.Invalidate(int64(dbID))
	}

	h.redirectWith(w, r, "Koneksi database berhasil dihapus", "", http.StatusOK)
}

func (h *AdminHandler) redirectWith(w http.ResponseWriter, r *http.Request, msg, errMsg string, apiStatusCode int) {
	// Cek jika pemanggil adalah API murni (bukan form browser)
	isHTMLForm := r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" ||
		strings.Contains(r.Header.Get("Accept"), "text/html")

	if !isHTMLForm && !strings.Contains(r.Header.Get("Referer"), "/admin") {
		if errMsg != "" {
			http.Error(w, errMsg, apiStatusCode)
		} else {
			w.WriteHeader(apiStatusCode)
			if msg != "" {
				w.Write([]byte(msg))
			}
		}
		return
	}

	// Redirect 303 kembali ke /admin dengan query string
	targetURL := "/admin"
	params := url.Values{}
	if msg != "" {
		params.Set("msg", msg)
	}
	if errMsg != "" {
		params.Set("err", errMsg)
	}
	if len(params) > 0 {
		targetURL += "?" + params.Encode()
	}

	http.Redirect(w, r, targetURL, http.StatusSeeOther)
}

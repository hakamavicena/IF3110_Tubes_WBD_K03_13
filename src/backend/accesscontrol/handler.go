package accesscontrol

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"backend/auth"
	"backend/dbengine/pkg/dbtarget"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,50}$`)

const maxAdminBody = 1 << 20

type AdminHandler struct {
	service            *AccessService
	repo               AccessRepository
	manager            *dbtarget.Manager
	box                *dbtarget.SecretBox
	sessionInvalidator func(userID int) error
	sessionRotator     func(w http.ResponseWriter, r *http.Request) (string, error)
}

func NewAdminHandler(service *AccessService, repo AccessRepository, manager *dbtarget.Manager, box *dbtarget.SecretBox) *AdminHandler {
	return &AdminHandler{
		service: service,
		repo: repo,
		manager: manager,
		box: box,
	}
}

func (h *AdminHandler) SetSessionInvalidator(f func(userID int) error) {
	h.sessionInvalidator = f
}

func (h *AdminHandler) SetSessionRotator(f func(w http.ResponseWriter, r *http.Request) (string, error)) {
	h.sessionRotator = f
}

func (h *AdminHandler) Register(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/admin/users", protect(h.ListUsers))
	mux.Handle("POST /api/admin/users", protect(h.CreateUser))
	mux.Handle("PATCH /api/admin/users/{id}/status", protect(h.SetUserStatus))
	mux.Handle("GET /api/admin/groups", protect(h.ListGroups))
	mux.Handle("POST /api/admin/groups", protect(h.CreateGroup))
	mux.Handle("PUT /api/admin/groups/{id}/leader", protect(h.SetGroupLeader))
	mux.Handle("POST /api/admin/groups/{id}/members", protect(h.AddGroupMember))
	mux.Handle("DELETE /api/admin/groups/{id}/members/{userID}", protect(h.RemoveGroupMember))
	mux.Handle("PUT /api/admin/groups/{id}/access", protect(h.SetGroupAccess))
	mux.Handle("GET /api/admin/targets", protect(h.ListTargets))
	mux.Handle("POST /api/admin/targets", protect(h.CreateTarget))
	mux.Handle("DELETE /api/admin/targets/{id}", protect(h.DeleteTarget))
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminBody)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "body request tidak valid")
		return false
	}
	return true
}

func pathID(r *http.Request, name string) (int, bool) {
	v, err := strconv.Atoi(r.PathValue(name))
	return v, err == nil && v > 0
}

func actorID(r *http.Request) int {
	id, _ := auth.UserIDFromContext(r.Context())
	return int(id)
}

func audit(r *http.Request, event string, attrs ...any) {
	base := []any{"event", event, "status", "success", "actor_id", actorID(r)}
	slog.InfoContext(r.Context(), "aktivitas admin", append(base, attrs...)...)
}

func serverError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	slog.ErrorContext(r.Context(), msg, "event", "admin.error", "status", "error", "path", r.URL.Path, "err", err)
	auth.WriteError(w, http.StatusInternalServerError, "terjadi kesalahan pada server")
}

func (h *AdminHandler) privilegeChanged(w http.ResponseWriter, r *http.Request, targetUserID int) (newCSRF string) {
	if targetUserID == actorID(r) && h.sessionRotator != nil {
		token, err := h.sessionRotator(w, r)
		if err != nil {
			slog.WarnContext(r.Context(), "gagal merotasi sesi", "event", "auth.rotate_session", "status", "failed", "err", err)
			return ""
		}
		return token
	}
	if h.sessionInvalidator != nil {
		if err := h.sessionInvalidator(targetUserID); err != nil {
			slog.WarnContext(r.Context(), "gagal mencabut sesi", "event", "auth.revoke_sessions", "status", "failed", "target_user_id", targetUserID, "err", err)
		}
	}
	return ""
}

func withCSRF(m map[string]any, token string) map[string]any {
	if token != "" {
		m["csrf_token"] = token
	}
	return m
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

type userJSON struct {
	ID       int     `json:"id"`
	Username string  `json:"username"`
	IsAdmin  bool    `json:"is_admin"`
	IsActive bool    `json:"is_active"`
	Groups   []Group `json:"groups"`
}

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.repo.GetAllUsers()
	if err != nil {
		serverError(w, r, "gagal mengambil daftar pengguna", err)
		return
	}
	out := make([]userJSON, 0, len(users))
	for _, u := range users {
		groups, err := h.repo.GetGroupsByUserID(u.ID)
		if err != nil {
			serverError(w, r, "gagal mengambil group pengguna", err)
			return
		}
		if groups == nil {
			groups = []Group{}
		}
		out = append(out, userJSON{ID: u.ID, Username: u.Username, IsAdmin: u.IsAdmin, IsActive: u.IsActive, Groups: groups})
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if !usernameRegex.MatchString(req.Username) {
		auth.WriteError(w, http.StatusBadRequest, "username harus 3-50 karakter alfanumerik atau _ . -")
		return
	}

	if len(req.Password) < 8 {
		auth.WriteError(w, http.StatusBadRequest, "password minimal 8 karakter")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		serverError(w, r, "gagal hash password", err)
		return
	}
	u, err = h.repo.CreateUserWithPassword(req.Username, string(hash), req.IsAdmin)
	if err != nil {
		if isUniqueViolation(err) {
			auth.WriteError(w, http.StatusConflict, "username sudah digunakan")
			return
		}
		serverError(w, r, "gagal membuat user", err)
		return
	}
	audit(r, "admin.user.create", "target_user_id", u.ID, "is_admin", u.IsAdmin)
	auth.WriteJSON(w, http.StatusCreated, userJSON{ID: u.ID, Username: u.Username, IsAdmin: u.IsAdmin, IsActive: u.IsActive, Groups: []Group{}})
}

func (h *AdminHandler) SetUserStatus(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathID(r, "id")
	if !ok {
		auth.WriteError(w, http.StatusBadRequest, "ID user tidak valid")
		return
	}
	var req struct {
		IsActive *bool `json:"is_active"`
		IsAdmin  *bool `json:"is_admin"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.IsActive == nil && req.IsAdmin == nil {
		auth.WriteError(w, http.StatusBadRequest, "isi is_active dan/atau is_admin")
		return
	}
	target, err := h.repo.GetUser(targetID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusNotFound, "user tidak ditemukan")
			return
		}
		serverError(w, r, "gagal mengambil user", err)
		return
	}
	current := actorID(r)

	losesAdmin := target.IsAdmin && target.IsActive &&
		((req.IsActive != nil && !*req.IsActive) || (req.IsAdmin != nil && !*req.IsAdmin))
	if req.IsActive != nil && !*req.IsActive && targetID == current {
		auth.WriteError(w, http.StatusBadRequest, "admin tidak boleh menonaktifkan dirinya sendiri")
		return
	}
	if losesAdmin {
		count, err := h.repo.GetActiveAdminCount()
		if err != nil {
			serverError(w, r, "gagal memverifikasi jumlah admin", err)
			return
		}
		if count <= 1 {
			auth.WriteError(w, http.StatusConflict, "tidak dapat menghapus/menonaktifkan admin terakhir")
			return
		}
	}

	csrf := ""
	if req.IsAdmin != nil && *req.IsAdmin != target.IsAdmin {
		if err := h.repo.SetUserAdmin(targetID, *req.IsAdmin); err != nil {
			serverError(w, r, "gagal mengubah status admin", err)
			return
		}
		csrf = h.privilegeChanged(w, r, targetID) // privilege berubah -> rotasi / cabut sesi
		audit(r, "admin.user.set_admin", "target_user_id", targetID, "is_admin", *req.IsAdmin)
	}
	if req.IsActive != nil && *req.IsActive != target.IsActive {
		if err := h.repo.SetUserActive(targetID, *req.IsActive); err != nil {
			serverError(w, r, "gagal mengubah status aktif", err)
			return
		}
		if !*req.IsActive && h.sessionInvalidator != nil {
			if err := h.sessionInvalidator(targetID); err != nil {
				slog.WarnContext(r.Context(), "gagal mencabut sesi", "event", "auth.revoke_sessions", "status", "failed", "target_user_id", targetID, "err", err)
			}
		}
		audit(r, "admin.user.set_active", "target_user_id", targetID, "is_active", *req.IsActive)
	}
	updated, err := h.repo.GetUser(targetID)
	if err != nil {
		serverError(w, r, "gagal membaca user", err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, withCSRF(map[string]any{"user": updated}, csrf))
}

type groupJSON struct {
	ID             int                  `json:"id"`
	Name           string               `json:"name"`
	LeaderID       *int                 `json:"leader_id"`
	LeaderUsername string               `json:"leader_username"`
	Members        []User               `json:"members"`
	Grants         []GroupDatabaseGrant `json:"grants"`
}

func (h *AdminHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.repo.GetAllGroups()
	if err != nil {
		serverError(w, r, "gagal mengambil daftar group", err)
		return
	}
	out := make([]groupJSON, 0, len(groups))
	for _, g := range groups {
		members, err := h.repo.GetGroupMembers(g.ID)
		if err != nil {
			serverError(w, r, "gagal mengambil anggota group", err)
			return
		}
		grants, err := h.repo.GetGroupGrants(g.ID)
		if err != nil {
			serverError(w, r, "gagal mengambil grant group", err)
			return
		}
		if members == nil {
			members = []User{}
		}
		if grants == nil {
			grants = []GroupDatabaseGrant{}
		}
		out = append(out, groupJSON{ID: g.ID, Name: g.Name, LeaderID: g.LeaderID, LeaderUsername: g.LeaderUsername, Members: members, Grants: grants})
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

func (h *AdminHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 100 {
		auth.WriteError(w, http.StatusBadRequest, "nama group wajib diisi (maks. 100 karakter)")
		return
	}
	g, err := h.repo.CreateGroup(req.Name)
	if err != nil {
		if isUniqueViolation(err) {
			auth.WriteError(w, http.StatusConflict, "nama group sudah digunakan")
			return
		}
		serverError(w, r, "gagal membuat group", err)
		return
	}
	audit(r, "admin.group.create", "group_id", g.ID)
	auth.WriteJSON(w, http.StatusCreated, groupJSON{ID: g.ID, Name: g.Name, Members: []User{}, Grants: []GroupDatabaseGrant{}})
}

func (h *AdminHandler) SetGroupLeader(w http.ResponseWriter, r *http.Request) {
	groupID, ok := pathID(r, "id")
	if !ok {
		auth.WriteError(w, http.StatusBadRequest, "ID group tidak valid")
		return
	}
	var req struct {
		UserID int `json:"user_id"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.UserID <= 0 {
		auth.WriteError(w, http.StatusBadRequest, "user_id tidak valid")
		return
	}
	group, err := h.repo.GetGroup(groupID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusNotFound, "group tidak ditemukan")
			return
		}
		serverError(w, r, "gagal mengambil group", err)
		return
	}
	user, err := h.repo.GetUser(req.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusBadRequest, "user tidak ditemukan")
			return
		}
		serverError(w, r, "gagal mengambil user", err)
		return
	}
	if !user.IsActive {
		auth.WriteError(w, http.StatusBadRequest, "leader harus berupa user aktif")
		return
	}
	member, err := h.repo.IsGroupMember(req.UserID, groupID)
	if err != nil {
		serverError(w, r, "gagal memeriksa keanggotaan", err)
		return
	}
	if !member {
		auth.WriteError(w, http.StatusBadRequest, "leader harus anggota group tersebut")
		return
	}
	oldLeader := group.LeaderID
	if err := h.repo.SetGroupLeader(groupID, req.UserID); err != nil {
		serverError(w, r, "gagal menetapkan leader", err)
		return
	}

	csrf := h.privilegeChanged(w, r, req.UserID)
	if oldLeader != nil && *oldLeader != req.UserID {
		if c := h.privilegeChanged(w, r, *oldLeader); c != "" {
			csrf = c
		}
	}
	audit(r, "admin.group.set_leader", "group_id", groupID, "leader_id", req.UserID)
	auth.WriteJSON(w, http.StatusOK, withCSRF(map[string]any{
		"group_id": groupID, "leader_id": req.UserID, "leader_username": user.Username,
	}, csrf))
}

func (h *AdminHandler) AddGroupMember(w http.ResponseWriter, r *http.Request) {
	groupID, ok := pathID(r, "id")
	if !ok {
		auth.WriteError(w, http.StatusBadRequest, "ID group tidak valid")
		return
	}
	var req struct {
		UserID int `json:"user_id"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.UserID <= 0 {
		auth.WriteError(w, http.StatusBadRequest, "user_id tidak valid")
		return
	}
	if _, err := h.repo.GetGroup(groupID); err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusNotFound, "group tidak ditemukan")
			return
		}
		serverError(w, r, "gagal mengambil group", err)
		return
	}
	if _, err := h.repo.GetUser(req.UserID); err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusBadRequest, "user tidak ditemukan")
			return
		}
		serverError(w, r, "gagal mengambil user", err)
		return
	}
	if err := h.repo.AddUserToGroup(req.UserID, groupID); err != nil {
		serverError(w, r, "gagal menambahkan user ke group", err)
		return
	}
	csrf := h.privilegeChanged(w, r, req.UserID)
	audit(r, "admin.group.add_member", "group_id", groupID, "target_user_id", req.UserID)
	auth.WriteJSON(w, http.StatusOK, withCSRF(map[string]any{"group_id": groupID, "user_id": req.UserID}, csrf))
}

func (h *AdminHandler) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	groupID, ok1 := pathID(r, "id")
	userID, ok2 := pathID(r, "userID")
	if !ok1 || !ok2 {
		auth.WriteError(w, http.StatusBadRequest, "ID group/user tidak valid")
		return
	}
	group, err := h.repo.GetGroup(groupID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusNotFound, "group tidak ditemukan")
			return
		}
		serverError(w, r, "gagal mengambil group", err)
		return
	}
	if group.LeaderID != nil && *group.LeaderID == userID {
		auth.WriteError(w, http.StatusConflict, "user adalah leader group; tetapkan leader lain terlebih dahulu")
		return
	}
	if err := h.repo.RemoveUserFromGroup(userID, groupID); err != nil {
		serverError(w, r, "gagal mengeluarkan user dari group", err)
		return
	}
	csrf := h.privilegeChanged(w, r, userID)
	audit(r, "admin.group.remove_member", "group_id", groupID, "target_user_id", userID)
	auth.WriteJSON(w, http.StatusOK, withCSRF(map[string]any{"group_id": groupID, "user_id": userID}, csrf))
}

func (h *AdminHandler) SetGroupAccess(w http.ResponseWriter, r *http.Request) {
	groupID, ok := pathID(r, "id")
	if !ok {
		auth.WriteError(w, http.StatusBadRequest, "ID group tidak valid")
		return
	}
	var req struct {
		DatabaseID  int `json:"database_id"`
		AccessLevel int `json:"access_level"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.DatabaseID <= 0 || req.AccessLevel < 0 || req.AccessLevel > 2 {
		auth.WriteError(w, http.StatusBadRequest, "database_id wajib dan access_level harus 0 (cabut), 1 (view), atau 2 (view+edit)")
		return
	}
	if _, err := h.repo.GetGroup(groupID); err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusNotFound, "group tidak ditemukan")
			return
		}
		serverError(w, r, "gagal mengambil group", err)
		return
	}
	if _, err := h.repo.GetDatabase(req.DatabaseID); err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusBadRequest, "database target tidak ditemukan")
			return
		}
		serverError(w, r, "gagal mengambil database", err)
		return
	}
	if req.AccessLevel == 0 {
		err := h.repo.DeleteGroupAccess(groupID, req.DatabaseID)
		if err != nil {
			serverError(w, r, "gagal mencabut akses", err)
			return
		}
	} else if err := h.repo.SetGroupAccess(groupID, req.DatabaseID, AccessLevel(req.AccessLevel)); err != nil {
		serverError(w, r, "gagal mengatur akses", err)
		return
	}
	audit(r, "admin.group.set_access", "group_id", groupID, "database_id", req.DatabaseID, "access_level", req.AccessLevel)
	auth.WriteJSON(w, http.StatusOK, GroupAccess{GroupID: groupID, DatabaseID: req.DatabaseID, AccessLevel: AccessLevel(req.AccessLevel)})
}

func isValidSSLMode(mode string) bool {
	switch mode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return true
	}
	return false
}

func (h *AdminHandler) ListTargets(w http.ResponseWriter, r *http.Request) {
	dbs, err := h.repo.GetAllDatabases()
	if err != nil {
		serverError(w, r, "gagal mengambil daftar target", err)
		return
	}
	if dbs == nil {
		dbs = []Database{}
	}
	auth.WriteJSON(w, http.StatusOK, dbs) // PasswordEnc ber-tag json:"-"
}

func (h *AdminHandler) CreateTarget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string `json:"name"`
		Host         string `json:"host"`
		Port         int    `json:"port"`
		DatabaseName string `json:"database_name"`
		Username     string `json:"username"`
		Password     string `json:"password"`
		SSLMode      string `json:"ssl_mode"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Name, req.Host = strings.TrimSpace(req.Name), strings.TrimSpace(req.Host)
	req.DatabaseName, req.Username = strings.TrimSpace(req.DatabaseName), strings.TrimSpace(req.Username)
	req.SSLMode = strings.TrimSpace(req.SSLMode)
	if req.SSLMode == "" {
		req.SSLMode = "prefer"
	}
	if req.Name == "" || req.Host == "" || req.DatabaseName == "" || req.Username == "" || req.Password == "" {
		auth.WriteError(w, http.StatusBadRequest, "name, host, database_name, username, dan password wajib diisi")
		return
	}
	if req.Port < 1 || req.Port > 65535 {
		auth.WriteError(w, http.StatusBadRequest, "port harus antara 1 dan 65535")
		return
	}
	if !isValidSSLMode(req.SSLMode) {
		auth.WriteError(w, http.StatusBadRequest, "ssl_mode tidak valid")
		return
	}
	target := &dbtarget.Target{Name: req.Name, Host: req.Host, Port: req.Port, DatabaseName: req.DatabaseName, Username: req.Username, SSLMode: req.SSLMode}
	if h.manager != nil {
		testCtx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		if err := h.manager.TestConnection(testCtx, target, req.Password); err != nil {
			slog.WarnContext(r.Context(), "uji koneksi target gagal", "event", "admin.target.test", "status", "failed", "target_name", req.Name, "err", err)
			auth.WriteError(w, http.StatusBadRequest, "gagal menghubungi database target; periksa host, port, kredensial, dan status database")
			return
		}
	}
	var passwordEnc []byte
	if h.box != nil {
		enc, err := h.box.Encrypt(req.Password)
		if err != nil {
			serverError(w, r, "gagal mengenkripsi password target", err)
			return
		}
		passwordEnc = enc
	}
	db := &Database{Name: req.Name, Host: req.Host, Port: req.Port, DatabaseName: req.DatabaseName, Username: req.Username, PasswordEnc: passwordEnc, SSLMode: req.SSLMode}
	if err := h.repo.CreateDatabase(db); err != nil {
		if isUniqueViolation(err) {
			auth.WriteError(w, http.StatusConflict, "nama target sudah digunakan")
			return
		}
		serverError(w, r, "gagal mendaftarkan target", err)
		return
	}
	audit(r, "admin.target.create", "database_id", db.ID)
	auth.WriteJSON(w, http.StatusCreated, db)
}

func (h *AdminHandler) DeleteTarget(w http.ResponseWriter, r *http.Request) {
	dbID, ok := pathID(r, "id")
	if !ok {
		auth.WriteError(w, http.StatusBadRequest, "ID database tidak valid")
		return
	}
	if _, err := h.repo.GetDatabase(dbID); err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusNotFound, "database target tidak ditemukan")
			return
		}
		serverError(w, r, "gagal mengambil target", err)
		return
	}
	if err := h.repo.DeleteDatabase(dbID); err != nil {
		serverError(w, r, "gagal menghapus target", err)
		return
	}
	if h.manager != nil {
		h.manager.Invalidate(int64(dbID))
	}
	audit(r, "admin.target.delete", "database_id", dbID)
	auth.WriteJSON(w, http.StatusOK, map[string]any{"deleted": dbID})
}

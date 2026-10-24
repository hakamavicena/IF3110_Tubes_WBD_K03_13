package accesscontrol

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type AdminHandler struct {
	service *AccessService
	repo    AccessRepository
}

func NewAdminHandler(service *AccessService, repo AccessRepository) *AdminHandler {
	return &AdminHandler{service: service, repo: repo}
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
	dbID, _ := strconv.Atoi(r.FormValue("db_id"))
	level, _ := strconv.Atoi(r.FormValue("access_level"))

	if err := h.repo.SetGroupAccess(groupID, dbID, AccessLevel(level)); err != nil {
		http.Error(w, "Gagal mengatur akses group", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Berhasil mengatur akses group"))
}

func (h *AdminHandler) CreateDatabaseConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	port, _ := strconv.Atoi(r.FormValue("port"))
	db := &Database{
		Name:     r.FormValue("name"),
		Host:     r.FormValue("host"),
		Port:     port,
		User:     r.FormValue("user"),
		Password: r.FormValue("password"),
		DBName:   r.FormValue("db_name"),
	}

	if err := h.repo.CreateDatabase(db); err != nil {
		http.Error(w, "Gagal mendaftarkan database", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(db)
}

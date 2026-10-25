package auth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"time"
)

type AuthHandler struct {
	service *AuthService
}

func NewAuthHandler(service *AuthService) *AuthHandler {
	return &AuthHandler{service: service}
}

func (h *AuthHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Pertahanan CSRF login: mewajibkan header kustom X-Requested-With (memicu preflight CORS) jika JSON
	if r.Header.Get("X-Requested-With") != "XMLHttpRequest" && r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Invalid request origin or headers", http.StatusForbidden)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Rotasi saat login: jika request membawa cookie sesi lama, hapus sesi lama
	if oldCookie, err := r.Cookie("session_id"); err == nil && oldCookie.Value != "" {
		_ = h.service.DeleteSession(oldCookie.Value)
	}

	session, err := h.service.Login(req.Username, req.Password)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Pengaturan Cookie sesuai spesifikasi keamanan: Secure kondisional (APP_ENV), SameSite=Lax
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    session.SessionID,
		Expires:  session.AbsoluteExpiration,
		HttpOnly: true,
		Secure:   h.service.config.SecureCookie,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "login successful"}`))
}

func (h *AuthHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie("session_id")
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	session, _, err := h.service.ValidateAndRotateSession(cookie.Value)
	if err != nil || session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Periksa token CSRF
	_ = r.ParseForm()
	token := r.FormValue("csrf_token")
	if token == "" {
		token = r.Header.Get("X-CSRF-Token")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(session.CSRFToken)) != 1 {
		http.Error(w, "Invalid or missing CSRF token", http.StatusForbidden)
		return
	}
	_ = h.service.DeleteSession(session.SessionID)

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.service.config.SecureCookie,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

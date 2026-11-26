package auth

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"mime"
	"net/http"
	"time"
)

const maxJSONBody = 1 << 20 
type LeaderGroupsFunc func(userID int) ([]LeaderGroup, error)

type AuthHandler struct {
	service *AuthService
	leaderGroups LeaderGroupsFunc
}

func NewAuthHandler(service *AuthService) *AuthHandler {
	return &AuthHandler{service: service}
}

func (h *AuthHandler) SetLeaderGroupsFunc(f LeaderGroupsFunc) { h.leaderGroups = f }

type authResponse struct {
	User         *User         `json:"user"`
	LeaderGroups []LeaderGroup `json:"leader_groups"`
	CSRFToken    string        `json:"csrf_token"`
}

func (h *AuthHandler) setSessionCookie(w http.ResponseWriter, id string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    id,
		Expires:  expires,
		HttpOnly: true,
		Secure:   h.service.config.SecureCookie,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
}

func (h *AuthHandler) clearSessionCookie(w http.ResponseWriter) {
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
}

func isJSONRequest(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

func (h *AuthHandler) buildResponse(userID int, csrf string) (*authResponse, error) {
	user, err := h.service.GetUser(userID)
	if err != nil {
		return nil, err
	}
	leaders := []LeaderGroup{}
	if h.leaderGroups != nil {
		lg, err := h.leaderGroups(userID)
		if err != nil {
			return nil, err
		}
		if lg != nil {
			leaders = lg
		}
	}
	return &authResponse{User: user, LeaderGroups: leaders, CSRFToken: csrf}, nil
}

func (h *AuthHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !isJSONRequest(r) {
		WriteError(w, http.StatusUnsupportedMediaType, "content-type harus application/json")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "body request tidak valid")
		return
	}

	// Rotasi saat login: jika request membawa cookie sesi lama, hapus sesi lama
	if oldCookie, err := r.Cookie("session_id"); err == nil && oldCookie.Value != "" {
		_ = h.service.DeleteSession(oldCookie.Value)
	}

	session, err := h.service.Login(req.Username, req.Password)
	if err != nil {
		slog.WarnContext(r.Context(), "login gagal", "event", "auth.login", "status", "failed", "actor", req.Username)
		WriteError(w, http.StatusUnauthorized, "username atau password salah")
		return
	}

	// Pengaturan Cookie sesuai spesifikasi keamanan: Secure kondisional (APP_ENV), SameSite=Lax
	h.setSessionCookie(w, session.SessionID, session.AbsoluteExpiration)
	resp, err := h.buildResponse(session.UserID, session.CSRFToken)
	if err != nil {
		slog.ErrorContext(r.Context(), "gagal menyusun profil login", "event", "auth.login", "status", "error", "err", err)
		WriteError(w, http.StatusInternalServerError, "terjadi kesalahan pada server")
		return
	}
	slog.InfoContext(r.Context(), "login berhasil", "event", "auth.login", "status", "success", "actor", resp.User.Username)
	WriteJSON(w, http.StatusOK, resp)
}

func (h *AuthHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cookie, err := r.Cookie("session_id")
	if err != nil || cookie.Value == "" {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	session, _, err := h.service.ValidateAndRotateSession(cookie.Value)
	if err != nil || session == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	token := r.Header.Get("X-CSRF-Token")
	if token == "" {
		_ = r.ParseForm()
		token = r.FormValue("csrf_token")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(session.CSRFToken)) != 1 {
		WriteError(w, http.StatusForbidden, "token CSRF tidak valid")
		return
	}
	_ = h.service.DeleteSession(session.SessionID)
	h.clearSessionCookie(w)
	slog.InfoContext(r.Context(), "logout", "event", "auth.logout", "status", "success", "user_id", session.UserID)
	WriteJSON(w, http.StatusOK, map[string]string{"message": "logout berhasil"})
}
	
func (h *AuthHandler) MeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := r.Context().Value(SessionContextKey).(*Session)
	if !ok || session == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	resp, err := h.buildResponse(session.UserID, session.CSRFToken)
	if err != nil {
		slog.ErrorContext(r.Context(), "gagal menyusun profil", "event", "auth.me", "status", "error", "err", err)
		WriteError(w, http.StatusInternalServerError, "terjadi kesalahan pada server")
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

func (h *AuthHandler) RotateCurrentSession(w http.ResponseWriter, r *http.Request) (string, error) {
	cookie, err := r.Cookie("session_id")
	if err != nil || cookie.Value == "" {
		return "", ErrSessionNotFound
	}
	session, err := h.service.RotateSession(cookie.Value)
	if err != nil {
		return "", err
	}
	h.setSessionCookie(w, session.SessionID, session.AbsoluteExpiration)
	return session.CSRFToken, nil
}
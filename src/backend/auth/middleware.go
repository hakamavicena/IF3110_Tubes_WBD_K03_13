package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
)

type contextKey string

const SessionContextKey = contextKey("session")

func (h *AuthHandler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		if err != nil || cookie.Value == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		session, newID, err := h.service.ValidateAndRotateSession(cookie.Value)
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "sesi berakhir atau tidak valid")
			return
		}

		// Jika rotasi berkala terjadi, kirim cookie baru dengan ID yang baru diterbitkan
		if newID != "" {
			h.setSessionCookie(w, newID, session.AbsoluteExpiration)
		}

		ctx := context.WithValue(r.Context(), SessionContextKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CSRFMiddleware memvalidasi token CSRF pada seluruh metode pengubah state (POST/PUT/PATCH/DELETE)
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			session, ok := r.Context().Value(SessionContextKey).(*Session)
			if !ok || session == nil {
				WriteError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			provided := r.Header.Get("X-CSRF-Token")
			if provided == "" && !isJSONRequest(r) {
				// Fallback form-urlencoded hanya untuk halaman HTML lama (non-SPA).
				_ = r.ParseForm()
				provided = r.FormValue("csrf_token")
			}

			if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) != 1 {
				WriteError(w, http.StatusForbidden, "token CSRF tidak valid atau tidak ada")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// CSRFToken mengekstrak token CSRF dari session di context
func CSRFToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if session, ok := r.Context().Value(SessionContextKey).(*Session); ok && session != nil {
		return session.CSRFToken
	}
	return ""
}

// UserIDFromContext mengekstrak user ID (int64) dari session yang tersimpan di context
func UserIDFromContext(ctx context.Context) (int64, bool) {
	if ctx == nil {
		return 0, false
	}
	session, ok := ctx.Value(SessionContextKey).(*Session)
	if !ok || session == nil || session.UserID <= 0 {
		return 0, false
	}
	return int64(session.UserID), true
}

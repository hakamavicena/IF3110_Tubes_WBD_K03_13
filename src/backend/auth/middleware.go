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
			http.Error(w, "Session expired or invalid", http.StatusUnauthorized)
			return
		}

		// Jika rotasi berkala terjadi, kirim cookie baru dengan ID yang baru diterbitkan
		if newID != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     "session_id",
				Value:    newID,
				Expires:  session.AbsoluteExpiration,
				HttpOnly: true,
				Secure:   h.service.config.SecureCookie,
				SameSite: http.SameSiteLaxMode,
				Path:     "/",
			})
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
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			_ = r.ParseForm()
			provided := r.FormValue("csrf_token")
			if provided == "" {
				provided = r.Header.Get("X-CSRF-Token")
			}

			if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) != 1 {
				http.Error(w, "Invalid or missing CSRF token", http.StatusForbidden)
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

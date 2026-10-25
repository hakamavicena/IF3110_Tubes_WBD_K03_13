package auth

import (
	"context"
	"net/http"
)

type contextKey string

const SessionContextKey = contextKey("session")

func (h *AuthHandler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		session, err := h.service.ValidateSession(cookie.Value)
		if err != nil {
			http.Error(w, "Session expired or invalid", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), SessionContextKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			csrfHeader := r.Header.Get("X-Requested-With")
			if csrfHeader != "XMLHttpRequest" {
				http.Error(w, "Potensi serangan CSRF terdeteksi", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

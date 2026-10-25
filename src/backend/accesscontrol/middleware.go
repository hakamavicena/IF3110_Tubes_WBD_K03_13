package accesscontrol

import (
	"backend/auth"
	"net/http"
	"strconv"
)

// Ini dipake barengan, buat bungkus endpoint hakam (view schema atau run migration)
// butuh requiredLevel (misal AccessViewEdit kalo mau migrasi
func RequireDBAccess(service *AccessService, requiredLevel AccessLevel, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := r.Context().Value(auth.SessionContextKey).(*auth.Session)
		if !ok || session == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		dbIDStr := r.URL.Query().Get("db_id")
		if dbIDStr == "" {
			http.Error(w, "Bad Request: db_id is required", http.StatusBadRequest)
			return
		}
		dbID, err := strconv.Atoi(dbIDStr)
		if err != nil || dbID <= 0 {
			http.Error(w, "Bad Request: invalid db_id", http.StatusBadRequest)
			return
		}

		effectiveAccess, err := service.GetEffectiveAccess(session.UserID, dbID)
		if err != nil || effectiveAccess < requiredLevel {
			http.Error(w, "Forbidden: Insufficient Access", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}
}

func RequireAdmin(service *AccessService, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := r.Context().Value(auth.SessionContextKey).(*auth.Session)
		if !ok || session == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if err := service.RequirePlatformAdmin(session.UserID); err != nil {
			http.Error(w, "Forbidden: Platform Admin only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}
}

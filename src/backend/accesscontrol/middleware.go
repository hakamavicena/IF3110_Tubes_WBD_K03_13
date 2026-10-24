package accesscontrol

import (
	"net/http"
	"strconv"
)

// Ini dipake barengan, buat bungkus endpoint hakam (view schema atau run migration)
// butuh requiredLevel (misal AccessViewEdit kalo mau migrasi
func RequireDBAccess(service *AccessService, requiredLevel AccessLevel, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// ini bagian auth, gw asumsikan ry ngelempar User ID lewat header 'X-User-ID' setelah dia ngecek session
		// nti kalo ry kelar, tinggal samain cara ngambil userID-nya
		userIDStr := r.Header.Get("X-User-ID") 

		if userIDStr == "" {
			ctxUserID := r.Context().Value("userID")
			if ctxUserID != nil {
				userIDStr = ctxUserID.(string)
			}
		}

		userID, err := strconv.Atoi(userIDStr)
		if err != nil {
			http.Error(w, "Unauthorized: Invalid User ID", http.StatusUnauthorized)
			return
		}

		// asumsi nangkep ID database 
		dbIDStr := r.URL.Query().Get("db_id")
		if dbIDStr == "" {
			http.Error(w, "Bad Request: db_id is required", http.StatusBadRequest)
			return
		}
		dbID, _ := strconv.Atoi(dbIDStr)
		effectiveAccess, err := service.GetEffectiveAccess(userID, dbID)
		if err != nil || effectiveAccess < requiredLevel {
			http.Error(w, "Forbidden: Insufficient Access", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}
}

func RequireAdmin(service *AccessService, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// sama kayak di atas, nunggu userID dari auth 
		userIDStr := r.Header.Get("X-User-ID")
		userID, err := strconv.Atoi(userIDStr)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if err := service.RequirePlatformAdmin(userID); err != nil {
			http.Error(w, "Forbidden: Platform Admin only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}
}
package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend/accesscontrol"
	"backend/auth"

	"backend/dbengine/pkg/dbtarget"
	"backend/dbengine/pkg/dbviewer"
	"backend/dbengine/pkg/httpdb"
	"backend/dbengine/pkg/migration"
	"backend/dbengine/pkg/ports"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

func findTemplatePath(filename string) string {
	candidates := []string{
		filepath.Join("..", "frontend", "templates", filename),
		filepath.Join("frontend", "templates", filename),
		filepath.Join("src", "frontend", "templates", filename),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join("..", "frontend", "templates", filename)
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Tidak ada file .env, menggunakan variabel environment OS")
	}

	internalDBURL := os.Getenv("INTERNAL_DB_URL")
	if internalDBURL == "" {
		host := os.Getenv("DB_HOST")
		if host == "" {
			host = "localhost"
		}
		port := os.Getenv("DB_PORT")
		if port == "" {
			port = "5432"
		}
		user := os.Getenv("DB_USER")
		pass := os.Getenv("DB_PASSWORD")
		dbname := os.Getenv("DB_NAME")
		if user == "" || dbname == "" {
			log.Fatal("INTERNAL_DB_URL atau (DB_USER dan DB_NAME) harus disetel pada environment variable")
		}
		if pass != "" {
			internalDBURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, pass, host, port, dbname)
		} else {
			internalDBURL = fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable", user, host, port, dbname)
		}
	}

	// 1. Inisialisasi koneksi SQL standar (Modul A & C)
	db, err := sql.Open("postgres", internalDBURL)
	if err != nil {
		log.Fatalf("Gagal membuka koneksi database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("Database internal tidak dapat dihubungi via sql.DB: %v", err)
	}

	// 2. Inisialisasi pgxpool (Modul B: viewer & migration)
	ctx := context.Background()
	pgxPool, err := pgxpool.New(ctx, internalDBURL)
	if err != nil {
		log.Fatalf("Gagal membuka koneksi pgxpool: %v", err)
	}
	defer pgxPool.Close()
	pingCtx, cancelPing := context.WithTimeout(ctx, 8*time.Second)
	defer cancelPing()
	if err := pgxPool.Ping(pingCtx); err != nil {
		log.Fatalf("Database internal tidak dapat dihubungi via pgxpool: %v", err)
	}

	// 3. Baca dan inisialisasi SECRET_KEY untuk enkripsi AES-256-GCM target DB
	secretKeyBase64 := os.Getenv("SECRET_KEY")
	if secretKeyBase64 == "" {
		log.Fatal("SECRET_KEY environment variable is required (base64 encoded 32 bytes)")
	}
	rawKey, err := base64.StdEncoding.DecodeString(secretKeyBase64)
	if err != nil || len(rawKey) != 32 {
		log.Fatal("SECRET_KEY must be a valid base64-encoded 32-byte key")
	}
	box, err := dbtarget.NewSecretBox(rawKey)
	if err != nil {
		log.Fatalf("Gagal inisialisasi secret box: %v", err)
	}

	// 4. Susun service modul A, C, dan B
	authRepo := auth.NewPostgresRepository(db)
	authConfig := auth.DefaultAuthConfig()
	authService := auth.NewAuthService(authRepo, authConfig)
	authHandler := auth.NewAuthHandler(authService)

	accessRepo := accesscontrol.NewPostgresRepository(db)
	accessService := accesscontrol.NewAccessService(accessRepo)

	targetRepo := dbtarget.NewPGTargetRepository(pgxPool)
	targetManager := dbtarget.NewManager(targetRepo, box)
	defer targetManager.Close()

	adminHandler := accesscontrol.NewAdminHandler(accessService, accessRepo, targetManager, box)
	adminHandler.SetTemplatePath(findTemplatePath("admin.html"))
	adminHandler.SetSessionInvalidator(func(userID int) error {
		return authService.InvalidateUserSessions(userID)
	})

	// Bootstrap Admin jika belum ada akun admin di database
	adminCount, err := accessRepo.GetActiveAdminCount()
	if err != nil {
		log.Fatalf("Gagal memeriksa status admin di database: %v", err)
	}
	if adminCount == 0 {
		adminUser := strings.TrimSpace(os.Getenv("ADMIN_USERNAME"))
		adminPass := os.Getenv("ADMIN_PASSWORD")
		if adminUser == "" || len(adminPass) < 8 {
			log.Fatal("Belum ada admin terdaftar. Setel variabel environment ADMIN_USERNAME dan ADMIN_PASSWORD (minimal 8 karakter) untuk bootstrap admin.")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("Gagal hash password bootstrap admin: %v", err)
		}
		_, err = accessRepo.CreateUserWithPassword(adminUser, string(hash), true)
		if err != nil {
			log.Fatalf("Gagal membuat user admin bootstrap: %v", err)
		}
		log.Printf("Bootstrap admin '%s' berhasil dibuat", adminUser)
	}

	authorizer := accesscontrol.NewAuthorizer(accessService, accessRepo)
	viewerService := dbviewer.NewService(authorizer, targetManager)

	migRepo := migration.NewPGRepository(pgxPool)
	timeout := 30 * time.Second
	if v := os.Getenv("MIGRATION_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			timeout = d
		}
	}
	migExecutor := &migration.Executor{StatementTimeout: timeout}
	migService := migration.NewService(migRepo, authorizer, targetManager, migExecutor, timeout)

	// Pemulihan migration yang stuck saat startup
	recoverCtx, cancelRecover := context.WithTimeout(ctx, 10*time.Second)
	recovered, err := migRepo.RecoverStuck(recoverCtx)
	cancelRecover()
	if err != nil {
		log.Printf("Peringatan: gagal memulihkan migration yang tertinggal: %v", err)
	} else if recovered > 0 {
		log.Printf("Berhasil menandai %d migration tertinggal (running) sebagai failed", recovered)
	}

	csrfTokenFunc := func(r *http.Request) string {
		return auth.CSRFToken(r)
	}

	httpdbHandler, err := httpdb.NewHandler(viewerService, migService, authorizer, csrfTokenFunc)
	if err != nil {
		log.Fatalf("Gagal inisialisasi httpdb handler: %v", err)
	}

	mux := http.NewServeMux()

	// Rute Publik & Autentikasi
	mux.HandleFunc("/login", authHandler.LoginHandler)
	mux.HandleFunc("/logout", authHandler.LogoutHandler)

	// Rute Dashboard
	dashboardHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := r.Context().Value(auth.SessionContextKey).(*auth.Session)
		if !ok || session == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		dbs, err := authorizer.AccessibleDatabases(r.Context(), int64(session.UserID))
		if err != nil {
			log.Printf("Gagal mengambil database yang dapat diakses: %v", err)
			dbs = nil
		}

		isAdmin := false
		if err := accessService.RequirePlatformAdmin(session.UserID); err == nil {
			isAdmin = true
		}

		tmplPath := findTemplatePath("dashboard.html")
		tmpl, err := template.ParseFiles(tmplPath)
		if err != nil {
			log.Printf("Gagal parse dashboard template (%s): %v", tmplPath, err)
			http.Error(w, "Gagal merender template", http.StatusInternalServerError)
			return
		}

		data := struct {
			UserID    int
			IsAdmin   bool
			Databases []ports.DatabaseAccess
			CSRFToken string
		}{
			UserID:    session.UserID,
			IsAdmin:   isAdmin,
			Databases: dbs,
			CSRFToken: auth.CSRFToken(r),
		}

		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("Gagal eksekusi dashboard template: %v", err)
			http.Error(w, "Gagal merender template", http.StatusInternalServerError)
		}
	})
	mux.Handle("/dashboard", authHandler.AuthMiddleware(dashboardHandler))

	// Rute Admin (khusus Platform Admin)
	adminGet := func(next http.HandlerFunc) http.Handler {
		return authHandler.AuthMiddleware(http.HandlerFunc(accesscontrol.RequireAdmin(accessService, next)))
	}
	adminPost := func(next http.HandlerFunc) http.Handler {
		return authHandler.AuthMiddleware(auth.CSRFMiddleware(http.HandlerFunc(accesscontrol.RequireAdmin(accessService, next))))
	}

	mux.Handle("/admin", adminGet(adminHandler.AdminDashboard))
	mux.Handle("/admin/users", adminPost(adminHandler.CreateUser))
	mux.Handle("/admin/users/status", adminPost(adminHandler.SetUserStatus))
	mux.Handle("/admin/groups", adminPost(adminHandler.CreateGroup))
	mux.Handle("/admin/users/groups", adminPost(adminHandler.AddUserToGroup))
	mux.Handle("/admin/users/groups/remove", adminPost(adminHandler.RemoveUserFromGroup))
	mux.Handle("/admin/groups/access", adminPost(adminHandler.SetGroupAccess))
	mux.Handle("/admin/databases", adminPost(adminHandler.CreateDatabaseConnection))
	mux.Handle("/admin/databases/delete", adminPost(adminHandler.DeleteDatabaseConnection))

	// Rute Database Engine (Viewer & Migration) dari modul B
	protectDB := func(next http.Handler) http.Handler {
		return authHandler.AuthMiddleware(auth.CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := auth.UserIDFromContext(r.Context())
			if !ok || userID <= 0 {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, httpdb.WithUserID(r, userID))
		})))
	}
	httpdbHandler.Register(mux, protectDB)

	// Rute Root (Halaman Login)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		tmplPath := findTemplatePath("login.html")
		tmpl, err := template.ParseFiles(tmplPath)
		if err != nil {
			log.Printf("Gagal parse login template (%s): %v", tmplPath, err)
			http.Error(w, "Gagal merender tampilan", http.StatusInternalServerError)
			return
		}

		if err := tmpl.Execute(w, nil); err != nil {
			log.Printf("Gagal eksekusi login template: %v", err)
			http.Error(w, "Gagal merender tampilan", http.StatusInternalServerError)
		}
	})

	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}
	fmt.Printf("Server berjalan di http://localhost%s...\n", listenAddr)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatal(err)
	}
}

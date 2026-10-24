package main

import (
	"backend/accesscontrol"
	"backend/auth"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"text/template"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Tidak ada file .env, menggunakan variabel environment OS")
	}
	connStr := fmt.Sprintf("user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
	)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Gagal membuka koneksi database:", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal("Database tidak dapat dihubungi:", err)
	}

	authRepo := auth.NewPostgresRepository(db)
	authService := auth.NewAuthService(authRepo)
	authHandler := auth.NewAuthHandler(authService)
	accessRepo := accesscontrol.NewPostgresRepository(db)
	accessService := accesscontrol.NewAccessService(accessRepo)
	adminHandler := accesscontrol.NewAdminHandler(accessService, accessRepo)

	mux := http.NewServeMux()
	mux.HandleFunc("/login", authHandler.LoginHandler)

	dashboardHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := r.Context().Value(auth.SessionContextKey).(*auth.Session)

		tmpl, err := template.ParseFiles("../frontend/templates/dashboard.html")
		if err != nil {
			http.Error(w, "Gagal merender template", http.StatusInternalServerError)
			return
		}

		if err := tmpl.Execute(w, session); err != nil {
			http.Error(w, "Gagal merender template", http.StatusInternalServerError)
		}
	})

	mux.Handle("/dashboard", authHandler.AuthMiddleware(dashboardHandler))
	mux.HandleFunc("/logout", authHandler.LogoutHandler)

	admin := func(next http.HandlerFunc) http.Handler {
		return authHandler.AuthMiddleware(http.HandlerFunc(accesscontrol.RequireAdmin(accessService, next)))
	}
	mux.Handle("/admin/users/groups", admin(adminHandler.AddUserToGroup))
	mux.Handle("/admin/groups/access", admin(adminHandler.SetGroupAccess))
	mux.Handle("/admin/databases", admin(adminHandler.CreateDatabaseConnection))

	fmt.Println("Server berjalan di port 8080...")

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		tmpl, err := template.ParseFiles("../frontend/templates/login.html")
		if err != nil {
			http.Error(w, "Gagal merender tampilan", http.StatusInternalServerError)
			return
		}

		if err := tmpl.Execute(w, nil); err != nil {
			http.Error(w, "Gagal merender tampilan", http.StatusInternalServerError)
		}
	})
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}

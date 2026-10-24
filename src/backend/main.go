package main

import (
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

	// Inisialisasi komponen Auth dengan PostgreSQL Repository
	repo := auth.NewPostgresRepository(db)
	service := auth.NewAuthService(repo)
	handler := auth.NewAuthHandler(service)

	mux := http.NewServeMux()
	mux.HandleFunc("/login", handler.LoginHandler)

	// protectedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	// 	session := r.Context().Value(auth.SessionContextKey).(*auth.Session)
	// 	w.Write([]byte(fmt.Sprintf("Akses aman! User ID: %d", session.UserID)))
	// })

	// mux.Handle("/protected", handler.AuthMiddleware(protectedHandler))
	// Handler untuk halaman dashboard
	dashboardHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := r.Context().Value(auth.SessionContextKey).(*auth.Session)

		tmpl, err := template.ParseFiles("../frontend/templates/dashboard.html")
		if err != nil {
			http.Error(w, "Gagal merender template", http.StatusInternalServerError)
			return
		}

		tmpl.Execute(w, session)
	})

	mux.Handle("/dashboard", handler.AuthMiddleware(dashboardHandler))
	mux.HandleFunc("/logout", handler.LogoutHandler) // Bisa juga dibungkus CSRFMiddleware jika diperlukan

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

		tmpl.Execute(w, nil)
	})
	http.ListenAndServe(":8080", mux)
}

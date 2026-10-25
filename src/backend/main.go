package main

import (
	"backend/accesscontrol"
	"backend/auth"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"github.com/hakamavicena/if3110-dbengine/pkg/dbtarget"
	"github.com/hakamavicena/if3110-dbengine/pkg/dbviewer"
	"github.com/hakamavicena/if3110-dbengine/pkg/httpdb"
	"github.com/hakamavicena/if3110-dbengine/pkg/migration"
)

func main() {
	godotenv.Load()
	connStr := fmt.Sprintf("user=%s password=%s dbname=%s sslmode=disable", os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"))

	db, _ := sql.Open("postgres", connStr)
	defer db.Close()

	pgxStr := fmt.Sprintf("postgres://%s:%s@localhost:5432/%s?sslmode=disable", os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"))
	pgxPool, _ := pgxpool.New(context.Background(), pgxStr)
	defer pgxPool.Close()

	authHandler := auth.NewAuthHandler(auth.NewAuthService(auth.NewPostgresRepository(db)))
	accessRepo := accesscontrol.NewPostgresRepository(db)
	accessService := accesscontrol.NewAccessService(accessRepo)
	adminHandler := accesscontrol.NewAdminHandler(accessService, accessRepo)

	key, _ := base64.StdEncoding.DecodeString(os.Getenv("SECRET_KEY")) // Pastikan .env punya SECRET_KEY 32-byte base64
	box, _ := dbtarget.NewSecretBox(key)
	targets := dbtarget.NewManager(dbtarget.NewPGTargetRepository(pgxPool), box)
	viewer := dbviewer.NewService(accessService, targets)
	migrations := migration.NewService(migration.NewPGRepository(pgxPool), accessService, targets, &migration.Executor{StatementTimeout: 30 * time.Second}, 30*time.Second)

	mux := http.NewServeMux()

	mux.HandleFunc("/login", authHandler.LoginHandler)
	mux.HandleFunc("/logout", authHandler.LogoutHandler)

	adminProtect := func(next http.HandlerFunc) http.Handler {
		return authHandler.AuthMiddleware(auth.CSRFMiddleware(http.HandlerFunc(accesscontrol.RequireAdmin(accessService, next))))
	}
	mux.Handle("/admin/dashboard", adminProtect(adminHandler.RenderAdminDashboard))
	mux.Handle("/admin/users/groups", adminProtect(adminHandler.AddUserToGroup))
	mux.Handle("/admin/groups/access", adminProtect(adminHandler.SetGroupAccess))
	mux.Handle("/admin/databases", adminProtect(adminHandler.CreateDatabaseConnection))

	dbHandler, _ := httpdb.NewHandler(viewer, migrations, accessService, func(r *http.Request) string {
		return "csrf-token-placeholder" 
	})
	dbHandler.Register(mux, func(next http.Handler) http.Handler {
		return authHandler.AuthMiddleware(auth.CSRFMiddleware(next))
	})

	fmt.Println("Server terintegrasi berjalan di port 8080...")
	http.ListenAndServe(":8080", mux)
}

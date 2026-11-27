package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"backend/accesscontrol"
	"backend/auth"

	"backend/dbengine/pkg/dbtarget"
	"backend/dbengine/pkg/dbviewer"
	"backend/dbengine/pkg/httpdb"
	"backend/dbengine/pkg/migration"
	"backend/dbengine/pkg/queue"
	"backend/dbengine/pkg/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	telemetry.SetupLogging()
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

	db, err := sql.Open("postgres", internalDBURL)
	if err != nil {
		log.Fatalf("Gagal membuka koneksi database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("Database internal tidak dapat dihubungi via sql.DB: %v", err)
	}

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

	authRepo := auth.NewPostgresRepository(db)
	authConfig := auth.DefaultAuthConfig()
	authService := auth.NewAuthService(authRepo, authConfig)
	authHandler := auth.NewAuthHandler(authService)

	accessRepo := accesscontrol.NewPostgresRepository(db)
	accessService := accesscontrol.NewAccessService(accessRepo)
	authHandler.SetLeaderGroupsFunc(func(userID int) ([]auth.LeaderGroup, error) {
		groups, err := accessRepo.GetLeaderGroups(userID)
		if err != nil {
			return nil, err
		}
		out := make([]auth.LeaderGroup, 0, len(groups))
		for _, g := range groups {
			out = append(out, auth.LeaderGroup{ID: g.ID, Name: g.Name})
		}
		return out, nil
	})

	targetRepo := dbtarget.NewPGTargetRepository(pgxPool)
	targetManager := dbtarget.NewManager(targetRepo, box)
	defer targetManager.Close()

	adminHandler := accesscontrol.NewAdminHandler(accessService, accessRepo, targetManager, box)
	adminHandler.SetSessionInvalidator(func(userID int) error {
		return authService.InvalidateUserSessions(userID)
	})
	adminHandler.SetSessionRotator(authHandler.RotateCurrentSession)

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
		if _, err = accessRepo.CreateUserWithPassword(adminUser, string(hash), true); err != nil {
			log.Fatalf("Gagal membuat user admin bootstrap: %v", err)
		}
		log.Printf("Bootstrap admin '%s' berhasil dibuat", adminUser)
	}

	authorizer := accesscontrol.NewAuthorizer(accessService, accessRepo)
	viewerService := dbviewer.NewService(authorizer, targetManager)

	// Observability: tracing (exporter console) + metrik Prometheus.
	shutdownTracing, err := telemetry.InitTracing(ctx, "dbplatform-backend")
	if err != nil {
		log.Printf("Peringatan: gagal inisialisasi tracing: %v", err)
		shutdownTracing = func(context.Context) error { return nil }
	}
	metrics := telemetry.NewMetrics()
	metrics.RegisterPool("internal", func() telemetry.PoolStat { return telemetry.PGXPoolStat(pgxPool) })

	migRepo := migration.NewPGRepository(pgxPool)
	timeout := 30 * time.Second
	if v := os.Getenv("MIGRATION_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			timeout = d
		}
	}
	migExecutor := &migration.Executor{StatementTimeout: timeout}
	migService := migration.NewService(migRepo, authorizer, targetManager, migExecutor, timeout)

	redisAddr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	redisPass := os.Getenv("REDIS_PASSWORD")
	redisQueueKey := strings.TrimSpace(os.Getenv("REDIS_QUEUE_KEY"))
	var jobQueue queue.Queue = queue.NewRedisQueue(redisAddr, redisPass, redisQueueKey)
	pingQueueCtx, cancelQueuePing := context.WithTimeout(ctx, 3*time.Second)
	if err := jobQueue.Ping(pingQueueCtx); err != nil {
		log.Printf("Peringatan: Redis (%s) belum dapat dihubungi (%v), menggunakan in-memory queue fallback", redisAddr, err)
		jobQueue = queue.NewMemoryQueue()
	}
	cancelQueuePing()
	defer jobQueue.Close()

	lockManager := migration.NewLockManager(pgxPool)
	jobCoordinator := migration.NewJobCoordinator(migRepo, lockManager, jobQueue, targetManager, migExecutor, authorizer, timeout)
	jobCoordinator.SetMetrics(metrics)

	// Pemulihan hanya menyentuh job yang lock-nya basi (heartbeat lewat batas).
	// Job milik instance lain yang masih hidup tidak disentuh.
	recoverCtx, cancelRecover := context.WithTimeout(ctx, 10*time.Second)
	if recoveredJobs, recErr := jobCoordinator.RecoverOnStartup(recoverCtx, 15*time.Second); recErr != nil {
		log.Printf("Peringatan: gagal memulihkan migration_jobs/locks saat startup: %v", recErr)
	} else if recoveredJobs > 0 {
		log.Printf("Berhasil memulihkan %d migration_jobs tertinggal saat startup", recoveredJobs)
	}
	cancelRecover()

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
	workers := jobCoordinator.StartWorkers(workerCtx, 3)

	httpdbHandler, err := httpdb.NewHandler(viewerService, migService, authorizer, auth.CSRFToken)
	if err != nil {
		log.Fatalf("Gagal inisialisasi httpdb handler: %v", err)
	}
	jobAPIHandler := httpdb.NewJobAPIHandler(jobCoordinator)
	approvalHandler := accesscontrol.NewApprovalHandler(
		accesscontrol.NewApprovalService(accesscontrol.NewPostgresApprovalStore(db), authorizer),
	)

	mux := http.NewServeMux()

	// Auth (JSON). Login tidak butuh sesi; logout/me memvalidasi sesi sendiri.
	mux.HandleFunc("POST /api/auth/login", authHandler.LoginHandler)
	mux.Handle("POST /api/auth/logout", authHandler.AuthMiddleware(auth.CSRFMiddleware(http.HandlerFunc(authHandler.LogoutHandler))))
	mux.Handle("GET /api/auth/me", authHandler.AuthMiddleware(http.HandlerFunc(authHandler.MeHandler)))

	// Admin: wajib login + CSRF (state-changing) + platform admin.
	adminProtect := func(next http.HandlerFunc) http.Handler {
		return authHandler.AuthMiddleware(auth.CSRFMiddleware(http.HandlerFunc(accesscontrol.RequireAdmin(accessService, next))))
	}
	adminHandler.Register(mux, adminProtect)

	// Viewer, history, migration/job, dan approval: login + CSRF + user ID di context.
	protectDB := func(next http.Handler) http.Handler {
		return authHandler.AuthMiddleware(auth.CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := auth.UserIDFromContext(r.Context())
			if !ok || userID <= 0 {
				auth.WriteError(w, http.StatusUnauthorized, "Unauthorized")
				return
			}
			next.ServeHTTP(w, httpdb.WithUserID(r, userID))
		})))
	}
	httpdbHandler.Register(mux, protectDB)
	jobAPIHandler.RegisterRoutes(mux, protectDB)
	approvalHandler.Register(mux, protectDB)

	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		hc, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pgxPool.Ping(hc); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := http.Handler(mux)
	handler = auth.CORS(os.Getenv("FRONTEND_ORIGIN"))(handler)
	handler = telemetry.Middleware(metrics)(handler)

	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}
	srv := &http.Server{Addr: listenAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-sigCtx.Done()
		slog.Info("shutdown dimulai")
		shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	slog.Info("server berjalan", "addr", listenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}

	cancelWorkers()
	workers.Wait()
	_ = shutdownTracing(context.Background())
}

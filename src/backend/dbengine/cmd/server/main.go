package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hakamavicena/if3110-dbengine/pkg/dbtarget"
	"github.com/hakamavicena/if3110-dbengine/pkg/dbviewer"
	"github.com/hakamavicena/if3110-dbengine/pkg/httpdb"
	"github.com/hakamavicena/if3110-dbengine/pkg/migration"
	"github.com/hakamavicena/if3110-dbengine/pkg/ports"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if os.Getenv("DEV_MODE") != "true" {
		return errors.New("standalone server is development-only; integrate httpdb with the platform auth (A) and access-control (C) modules")
	}
	address := os.Getenv("LISTEN_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	if err := requireLoopback(address); err != nil {
		return err
	}
	internalURL := os.Getenv("INTERNAL_DB_URL")
	if internalURL == "" {
		return errors.New("INTERNAL_DB_URL is required")
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("SECRET_KEY"))
	if err != nil {
		return fmt.Errorf("decode SECRET_KEY: %w", err)
	}
	box, err := dbtarget.NewSecretBox(key)
	if err != nil {
		return err
	}
	timeout := 30 * time.Second
	if value := os.Getenv("MIGRATION_TIMEOUT"); value != "" {
		timeout, err = time.ParseDuration(value)
		if err != nil || timeout <= 0 {
			return errors.New("MIGRATION_TIMEOUT must be a positive duration")
		}
	}

	ctx := context.Background()
	internal, err := pgxpool.New(ctx, internalURL)
	if err != nil {
		return fmt.Errorf("open internal database: %w", err)
	}
	defer internal.Close()
	pingCtx, cancelPing := context.WithTimeout(ctx, 8*time.Second)
	defer cancelPing()
	if err := internal.Ping(pingCtx); err != nil {
		return fmt.Errorf("ping internal database: %w", err)
	}

	targets := dbtarget.NewManager(dbtarget.NewPGTargetRepository(internal), box)
	defer targets.Close()
	repository := migration.NewPGRepository(internal)
	recoverCtx, cancelRecover := context.WithTimeout(ctx, 10*time.Second)
	recovered, err := repository.RecoverStuck(recoverCtx)
	cancelRecover()
	if err != nil {
		return fmt.Errorf("recover interrupted migrations: %w", err)
	}
	if recovered > 0 {
		log.Printf("marked %d interrupted migrations as failed", recovered)
	}

	authz := ports.AllowAll{}
	viewer := dbviewer.NewService(authz, targets)
	migrations := migration.NewService(repository, authz, targets,
		&migration.Executor{StatementTimeout: timeout}, timeout)
	csrfKey := make([]byte, 32)
	if _, err := rand.Read(csrfKey); err != nil {
		return fmt.Errorf("initialize development CSRF key: %w", err)
	}
	csrfToken := func() string {
		mac := hmac.New(sha256.New, csrfKey)
		_, _ = mac.Write([]byte("development-user-1"))
		return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	}
	handler, err := httpdb.NewHandler(viewer, migrations, authz, func(*http.Request) string {
		return csrfToken()
	})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	handler.Register(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
				if err := r.ParseForm(); err != nil {
					http.Error(w, "Invalid form", http.StatusBadRequest)
					return
				}
				provided := r.FormValue("csrf_token")
				expected := csrfToken()
				if !hmac.Equal([]byte(provided), []byte(expected)) {
					http.Error(w, "Invalid CSRF token", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, httpdb.WithUserID(r, 1))
		})
	})

	server := &http.Server{
		Addr: address, Handler: mux, ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()
	log.Printf("development database engine listening on http://%s", address)
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signalCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func requireLoopback(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid LISTEN_ADDR: %w", err)
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		return errors.New("development server must bind to an explicit loopback IP address")
	}
	return nil
}

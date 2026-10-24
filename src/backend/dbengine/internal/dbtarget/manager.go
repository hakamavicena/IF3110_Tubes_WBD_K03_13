package dbtarget

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/hakamavicena/if3110-dbengine/internal/apperr"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Target struct {
	ID           int64
	Name         string
	Host         string
	Port         int
	DatabaseName string
	Username     string
	PasswordEnc  []byte
	SSLMode      string
}

type TargetRepository interface {
	GetTarget(ctx context.Context, id int64) (*Target, error)
}

type Manager struct {
	repo  TargetRepository
	box   *SecretBox
	mu    sync.Mutex
	pools map[int64]*pgxpool.Pool
}

func NewManager(repo TargetRepository, box *SecretBox) *Manager {
	return &Manager{repo: repo, box: box, pools: make(map[int64]*pgxpool.Pool)}
}

func buildConnString(t *Target, password string) string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(t.Username, password),
		Host:     net.JoinHostPort(t.Host, strconv.Itoa(t.Port)),
		Path:     "/" + t.DatabaseName,
		RawQuery: "sslmode=" + url.QueryEscape(t.SSLMode),
	}
	return u.String()
}

func openPool(ctx context.Context, t *Target, password string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(buildConnString(t, password))
	if err != nil {
		return nil, fmt.Errorf("%w: konfigurasi koneksi tidak valid", apperr.ErrConnectionFailed)
	}
	cfg.MaxConns = 5
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.ConnConfig.RuntimeParams["application_name"] = "dbplatform"

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		slog.Warn("open target pool failed", "target_id", t.ID, "err_type", fmt.Sprintf("%T", err))
		return nil, apperr.ErrConnectionFailed
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		slog.Warn("ping target failed", "target_id", t.ID)
		return nil, apperr.ErrConnectionFailed
	}
	return pool, nil
}

func (m *Manager) Pool(ctx context.Context, id int64) (*pgxpool.Pool, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%w: database id tidak valid", apperr.ErrInvalidInput)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if pool, ok := m.pools[id]; ok {
		return pool, nil
	}
	if m.repo == nil || m.box == nil {
		return nil, fmt.Errorf("target manager is not configured")
	}
	target, err := m.repo.GetTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	password, err := m.box.Decrypt(target.PasswordEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt target password: %w", err)
	}
	pool, err := openPool(ctx, target, password)
	if err != nil {
		return nil, err
	}
	m.pools[id] = pool
	return pool, nil
}

func (m *Manager) TestConnection(ctx context.Context, target *Target, plainPassword string) error {
	if target == nil {
		return fmt.Errorf("%w: target tidak valid", apperr.ErrInvalidInput)
	}
	pool, err := openPool(ctx, target, plainPassword)
	if err != nil {
		return err
	}
	pool.Close()
	return nil
}

func (m *Manager) Invalidate(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pool, ok := m.pools[id]; ok {
		pool.Close()
		delete(m.pools, id)
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, pool := range m.pools {
		pool.Close()
		delete(m.pools, id)
	}
}

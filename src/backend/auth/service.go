package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrSessionExpired     = errors.New("session expired")
)

type AuthConfig struct {
	IdleTimeout      time.Duration
	AbsoluteTimeout  time.Duration
	RotationInterval time.Duration
	GracePeriod      time.Duration
	AppEnv           string
	SecureCookie     bool
}

func DefaultAuthConfig() AuthConfig {
	idle := 30 * time.Minute
	if v := os.Getenv("SESSION_IDLE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			idle = d
		}
	}
	abs := 12 * time.Hour
	if v := os.Getenv("SESSION_ABSOLUTE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			abs = d
		}
	}
	rot := 15 * time.Minute
	if v := os.Getenv("SESSION_ROTATION_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			rot = d
		}
	}
	grace := 30 * time.Second
	if v := os.Getenv("SESSION_GRACE_PERIOD"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			grace = d
		}
	}
	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "development"
	}
	return AuthConfig{
		IdleTimeout:      idle,
		AbsoluteTimeout:  abs,
		RotationInterval: rot,
		GracePeriod:      grace,
		AppEnv:           appEnv,
		SecureCookie:     appEnv == "production",
	}
}

type AuthService struct {
	repo   Repository
	config AuthConfig
	now    func() time.Time
}

func NewAuthService(repo Repository, config AuthConfig) *AuthService {
	return &AuthService{
		repo:   repo,
		config: config,
		now:    time.Now,
	}
}

func (s *AuthService) SetNowFunc(f func() time.Time) {
	s.now = f
}

func GenerateRandomToken(bytesLen int) (string, error) {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *AuthService) Login(username, password string) (*Session, error) {
	user, err := s.repo.GetUserByUsername(username)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	sessID, err := GenerateRandomToken(32)
	if err != nil {
		return nil, err
	}
	csrfToken, err := GenerateRandomToken(32)
	if err != nil {
		return nil, err
	}

	now := s.now()
	session := &Session{
		SessionID:          sessID,
		UserID:             user.ID,
		CSRFToken:          csrfToken,
		CreatedAt:          now,
		LastActivity:       now,
		AbsoluteExpiration: now.Add(s.config.AbsoluteTimeout),
	}

	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return session, nil
}

// ValidateAndRotateSession memvalidasi sesi dan melakukan rotasi berkala serta grace period
func (s *AuthService) ValidateAndRotateSession(sessionID string) (*Session, string, error) {
	session, err := s.repo.GetSession(sessionID)
	if err != nil {
		return nil, "", err
	}

	now := s.now()

	// 1. Periksa absolute expiration
	if now.After(session.AbsoluteExpiration) {
		_ = s.repo.DeleteSession(session.SessionID)
		return nil, "", ErrSessionExpired
	}

	// 2. Periksa idle timeout
	if now.Sub(session.LastActivity) > s.config.IdleTimeout {
		_ = s.repo.DeleteSession(session.SessionID)
		return nil, "", ErrSessionExpired
	}

	// 3. Jika request cocok dengan ID LAMA (previous_id):
	if sessionID == session.PreviousID {
		if now.After(session.PreviousValidUntil) {
			return nil, "", ErrSessionExpired
		}
		// Masih dalam grace period -> terima, TIDAK memicu rotasi lagi
		session.LastActivity = now
		_ = s.repo.SaveSession(session)
		return session, "", nil
	}

	// 4. Request cocok dengan session_id aktif: perbarui last activity
	session.LastActivity = now

	// 5. Periksa apakah rotasi berkala sudah saatnya
	if now.Sub(session.CreatedAt) > s.config.RotationInterval {
		newID, err := GenerateRandomToken(32)
		if err != nil {
			return nil, "", err
		}
		// Hapus kunci lama dari penyimpanan
		oldID := session.SessionID
		_ = s.repo.DeleteSession(oldID)

		session.PreviousID = oldID
		session.PreviousValidUntil = now.Add(s.config.GracePeriod)
		session.SessionID = newID
		session.CreatedAt = now

		if err := s.repo.SaveSession(session); err != nil {
			return nil, "", err
		}
		return session, newID, nil
	}

	if err := s.repo.SaveSession(session); err != nil {
		return nil, "", err
	}
	return session, "", nil
}

// RotateSession menerbitkan ID baru secara manual (misal saat privilege naik)
func (s *AuthService) RotateSession(oldSessionID string) (*Session, error) {
	session, err := s.repo.GetSession(oldSessionID)
	if err != nil {
		return nil, err
	}

	now := s.now()
	if now.After(session.AbsoluteExpiration) {
		_ = s.repo.DeleteSession(oldSessionID)
		return nil, ErrSessionExpired
	}

	newID, err := GenerateRandomToken(32)
	if err != nil {
		return nil, err
	}

	_ = s.repo.DeleteSession(session.SessionID)
	session.PreviousID = session.SessionID
	session.PreviousValidUntil = now.Add(s.config.GracePeriod)
	session.SessionID = newID
	session.CreatedAt = now
	session.LastActivity = now

	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *AuthService) InvalidateUserSessions(userID int) error {
	return s.repo.DeleteUserSessions(userID)
}

func (s *AuthService) DeleteSession(sessionID string) error {
	return s.repo.DeleteSession(sessionID)
}

func (s *AuthService) Config() AuthConfig {
	return s.config
}

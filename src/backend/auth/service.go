package auth

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCredentials = errors.New("invalid username or password")
var ErrSessionExpired = errors.New("session expired")

type AuthService struct {
	repo Repository
}

func NewAuthService(repo Repository) *AuthService {
	return &AuthService{repo: repo}
}

func (s *AuthService) Login(username, password string) (*Session, error) {
	user, err := s.repo.GetUserByUsername(username)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	now := time.Now()
	session := &Session{
		SessionID:          uuid.New().String(),
		UserID:             user.ID,
		CreatedAt:          now,
		LastActivity:       now,
		AbsoluteExpiration: now.Add(24 * time.Hour),
	}

	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *AuthService) ValidateSession(sessionID string) (*Session, error) {
	session, err := s.repo.GetSession(sessionID)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	if now.After(session.AbsoluteExpiration) {
		s.repo.DeleteSession(sessionID)
		return nil, ErrSessionExpired
	}

	if now.Sub(session.LastActivity) > 30*time.Minute {
		s.repo.DeleteSession(sessionID)
		return nil, ErrSessionExpired
	}

	session.LastActivity = now
	s.repo.SaveSession(session)

	return session, nil
}

func (s *AuthService) RotateSession(oldSessionID string) (*Session, error) {
	oldSession, err := s.repo.GetSession(oldSessionID)
	if err != nil {
		return nil, err
	}

	if time.Now().After(oldSession.AbsoluteExpiration) {
		s.repo.DeleteSession(oldSessionID)
		return nil, ErrSessionExpired
	}

	newSession := &Session{
		SessionID:          uuid.New().String(),
		UserID:             oldSession.UserID,
		CreatedAt:          time.Now(),
		LastActivity:       time.Now(),
		AbsoluteExpiration: oldSession.AbsoluteExpiration,
	}

	s.repo.SaveSession(newSession)
	s.repo.DeleteSession(oldSessionID)

	return newSession, nil
}

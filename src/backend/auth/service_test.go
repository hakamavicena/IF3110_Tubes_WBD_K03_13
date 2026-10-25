package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestLogin_Success(t *testing.T) {
	mockRepo := NewMockRepository()

	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("rahasia123"), bcrypt.DefaultCost)

	mockRepo.Users["admin"] = &User{
		ID:       1,
		Username: "admin",
		Password: string(hashedPassword),
	}

	service := NewAuthService(mockRepo)

	session, err := service.Login("admin", "rahasia123")

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if session == nil {
		t.Fatalf("Expected session, got nil")
	}
	if session.UserID != 1 {
		t.Errorf("Expected UserID 1, got %d", session.UserID)
	}
}

func TestLogin_InvalidPassword(t *testing.T) {
	mockRepo := NewMockRepository()
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("rahasia123"), bcrypt.DefaultCost)

	mockRepo.Users["admin"] = &User{
		ID:       1,
		Username: "admin",
		Password: string(hashedPassword),
	}

	service := NewAuthService(mockRepo)

	// ini coba kalo password salah
	session, err := service.Login("admin", "salahpassword")

	if err != ErrInvalidCredentials {
		t.Errorf("Expected ErrInvalidCredentials, got %v", err)
	}
	if session != nil {
		t.Errorf("Expected session to be nil, got %v", session)
	}
}

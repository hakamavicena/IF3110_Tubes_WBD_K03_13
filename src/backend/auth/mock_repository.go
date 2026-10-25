package auth

// MockRepository ini buat test aja, jadi sebenernya ga kepake
type MockRepository struct {
	Users    map[string]*User
	Sessions map[string]*Session
}

func NewMockRepository() *MockRepository {
	return &MockRepository{
		Users:    make(map[string]*User),
		Sessions: make(map[string]*Session),
	}
}

func (m *MockRepository) GetUserByUsername(username string) (*User, error) {
	if user, exists := m.Users[username]; exists {
		return user, nil
	}
	return nil, ErrUserNotFound
}

func (m *MockRepository) SaveSession(session *Session) error {
	m.Sessions[session.SessionID] = session
	return nil
}

func (m *MockRepository) GetSession(sessionID string) (*Session, error) {
	if session, exists := m.Sessions[sessionID]; exists {
		return session, nil
	}
	return nil, ErrSessionNotFound
}

func (m *MockRepository) DeleteSession(sessionID string) error {
	delete(m.Sessions, sessionID)
	return nil
}

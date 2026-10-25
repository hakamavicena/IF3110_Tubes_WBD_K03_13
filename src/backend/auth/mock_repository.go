package auth

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
	for _, s := range m.Sessions {
		if s.PreviousID == sessionID {
			return s, nil
		}
	}
	return nil, ErrSessionNotFound
}

func (m *MockRepository) DeleteSession(sessionID string) error {
	delete(m.Sessions, sessionID)
	for id, s := range m.Sessions {
		if s.PreviousID == sessionID {
			delete(m.Sessions, id)
		}
	}
	return nil
}

func (m *MockRepository) DeleteUserSessions(userID int) error {
	for id, s := range m.Sessions {
		if s.UserID == userID {
			delete(m.Sessions, id)
		}
	}
	return nil
}

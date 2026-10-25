package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session has expired")
)

const (
	DefaultSessionTTL = 24 * time.Hour
	SessionCookieName = "clouddump_session"
)

// Session represents an active authenticated session for a user account.
type Session struct {
	Token     string    `json:"token"`
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// IsExpired checks whether the session TTL has elapsed.
func (s *Session) IsExpired() bool {
	return time.Now().UTC().After(s.ExpiresAt)
}

// SessionStore maintains in-memory sessions securely.
type SessionStore struct {
	sessions map[string]*Session
	ttl      time.Duration
	mu       sync.RWMutex
}

// NewSessionStore creates a thread-safe SessionStore.
func NewSessionStore(ttl time.Duration) *SessionStore {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return &SessionStore{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}
}

// CreateSession generates a high-entropy cryptographically random session token.
func (s *SessionStore) CreateSession(userID string) (*Session, error) {
	if userID == "" {
		return nil, errors.New("user ID cannot be empty")
	}

	rawBytes := make([]byte, 32) // 256-bit cryptographically secure random token
	if _, err := rand.Read(rawBytes); err != nil {
		return nil, fmt.Errorf("failed generating session token: %w", err)
	}

	token := hex.EncodeToString(rawBytes)
	now := time.Now().UTC()
	sess := &Session{
		Token:     token,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = sess

	return sess, nil
}

// GetSession retrieves and validates an active session by token.
func (s *SessionStore) GetSession(token string) (*Session, error) {
	if token == "" {
		return nil, ErrSessionNotFound
	}

	s.mu.RLock()
	sess, ok := s.sessions[token]
	s.mu.RUnlock()

	if !ok {
		return nil, ErrSessionNotFound
	}

	if sess.IsExpired() {
		s.RevokeSession(token)
		return nil, ErrSessionExpired
	}

	return sess, nil
}

// RevokeSession destroys a session upon logout.
func (s *SessionStore) RevokeSession(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// RevokeAllForUser terminates all active sessions for a user ID.
func (s *SessionStore) RevokeAllForUser(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, sess := range s.sessions {
		if sess.UserID == userID {
			delete(s.sessions, token)
		}
	}
}

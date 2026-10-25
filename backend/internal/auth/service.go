package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/database"
)

var (
	ErrUserAlreadyExists = errors.New("a user with this email already exists")
	ErrInvalidEmail      = errors.New("email cannot be empty")
)

// AuthService handles account registration, credentials verification, and session management.
type AuthService struct {
	userRepo     database.UserRepository
	sessionStore *SessionStore
}

// NewAuthService constructs an AuthService.
func NewAuthService(repo database.UserRepository, store *SessionStore) *AuthService {
	if store == nil {
		store = NewSessionStore(DefaultSessionTTL)
	}
	return &AuthService{
		userRepo:     repo,
		sessionStore: store,
	}
}

// Register creates a new user account with a hashed password and automatically logs them in.
func (s *AuthService) Register(ctx context.Context, email, password, name string) (*database.User, *Session, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, nil, ErrInvalidEmail
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	// Check if user already exists
	existing, err := s.userRepo.GetUserByEmail(ctx, email)
	if err == nil && existing != nil {
		return nil, nil, ErrUserAlreadyExists
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	userID := fmt.Sprintf("usr_%d", now.UnixNano())
	user := &database.User{
		ID:           userID,
		Email:        email,
		PasswordHash: hash,
		Name:         name,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.userRepo.CreateUser(ctx, user); err != nil {
		return nil, nil, fmt.Errorf("failed to create user: %w", err)
	}

	session, err := s.sessionStore.CreateSession(user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create session: %w", err)
	}

	return user, session, nil
}

// Login verifies password credentials and creates a new session.
func (s *AuthService) Login(ctx context.Context, email, password string) (*database.User, *Session, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return nil, nil, ErrInvalidPassword
	}

	user, err := s.userRepo.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, nil, ErrInvalidPassword
		}
		return nil, nil, err
	}

	if !CheckPasswordHash(password, user.PasswordHash) {
		return nil, nil, ErrInvalidPassword
	}

	session, err := s.sessionStore.CreateSession(user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed creating session: %w", err)
	}

	return user, session, nil
}

// ValidateSession resolves an active session token to its corresponding User.
func (s *AuthService) ValidateSession(ctx context.Context, token string) (*database.User, error) {
	sess, err := s.sessionStore.GetSession(token)
	if err != nil {
		return nil, err
	}

	user, err := s.userRepo.GetUserByID(ctx, sess.UserID)
	if err != nil {
		return nil, fmt.Errorf("user not found for valid session: %w", err)
	}

	return user, nil
}

// Logout terminates the given session token.
func (s *AuthService) Logout(token string) {
	s.sessionStore.RevokeSession(token)
}

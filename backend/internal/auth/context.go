package auth

import (
	"context"
	"net/http"

	"github.com/bwithraju/clouddump/backend/internal/database"
)

type userContextKeyType struct{}

var userContextKey = userContextKeyType{}

// WithUser associates the authenticated user record with the request context.
func WithUser(ctx context.Context, user *database.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext extracts the authenticated user from the context if present.
func UserFromContext(ctx context.Context) (*database.User, bool) {
	if ctx == nil {
		return nil, false
	}
	user, ok := ctx.Value(userContextKey).(*database.User)
	return user, ok && user != nil
}

// UserIDFromContext retrieves the authenticated user's ID, returning empty string if unauthenticated.
func UserIDFromContext(ctx context.Context) string {
	if user, ok := UserFromContext(ctx); ok && user != nil {
		return user.ID
	}
	return ""
}

// GetEffectiveUserID extracts the user ID from the request context if authenticated,
// falling back to the X-User-ID header (for backward compatibility in tests).
func GetEffectiveUserID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if user, ok := UserFromContext(r.Context()); ok && user != nil {
		return user.ID
	}
	return r.Header.Get("X-User-ID")
}


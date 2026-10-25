package auth

import (
	"encoding/json"
	"net/http"
	"strings"
)

// ExtractToken retrieves the session token from either the HTTP-only cookie or Bearer Authorization header.
func ExtractToken(r *http.Request) string {
	// 1. Check HTTP-only cookie
	if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// 2. Check Authorization Bearer header
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}

	return ""
}

// AuthenticateMiddleware extracts any session token, validates the user, and enriches request context.
// Does not reject unauthenticated requests; downstream handlers or RequireAuth enforce authorization.
func AuthenticateMiddleware(authService *AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ExtractToken(r)
			if token != "" && authService != nil {
				user, err := authService.ValidateSession(r.Context(), token)
				if err == nil && user != nil {
					r = r.WithContext(WithUser(r.Context(), user))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth wraps a handler ensuring only authenticated requests can proceed, returning 401 Unauthorized otherwise.
func RequireAuth(authService *AuthService, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// First check if user is already in context from AuthenticateMiddleware
		if _, ok := UserFromContext(r.Context()); ok {
			next(w, r)
			return
		}

		// Fallback token extraction and validation
		token := ExtractToken(r)
		if token == "" || authService == nil {
			respondUnauthorized(w, "authentication required")
			return
		}

		user, err := authService.ValidateSession(r.Context(), token)
		if err != nil || user == nil {
			respondUnauthorized(w, "invalid or expired session")
			return
		}

		next(w, r.WithContext(WithUser(r.Context(), user)))
	}
}

func respondUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   "unauthorized",
		"message": message,
	})
}

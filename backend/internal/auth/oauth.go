package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/database"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var (
	ErrOAuthNotConfigured = errors.New("google oauth is not configured (missing client id or secret)")
	ErrOAuthTokenNotFound = errors.New("no oauth token found for user")
)

const (
	GoogleDriveFileScope = "https://www.googleapis.com/auth/drive.file"
	GoogleDriveScope     = "https://www.googleapis.com/auth/drive"
	ProviderGoogleDrive  = "google_drive"
)

// GoogleOAuthConfig holds OAuth 2.0 configuration for Google Drive.
// Credentials remain strictly on the backend and are never sent to the client.
type GoogleOAuthConfig struct {
	oauthConfig *oauth2.Config
}

// NewGoogleOAuthConfig initializes OAuth configuration from environment variables.
func NewGoogleOAuthConfig() *GoogleOAuthConfig {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	redirectURI := os.Getenv("GOOGLE_REDIRECT_URI")

	if redirectURI == "" {
		redirectURI = "http://localhost:8080/api/oauth/google/callback"
	}

	conf := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURI,
		Scopes:       []string{GoogleDriveFileScope},
		Endpoint:     google.Endpoint,
	}

	return &GoogleOAuthConfig{oauthConfig: conf}
}

// NewCustomGoogleOAuthConfig allows injecting custom parameters (e.g. for testing).
func NewCustomGoogleOAuthConfig(clientID, clientSecret, redirectURI string, scopes []string, endpoint oauth2.Endpoint) *GoogleOAuthConfig {
	if len(scopes) == 0 {
		scopes = []string{GoogleDriveFileScope}
	}
	conf := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURI,
		Scopes:       scopes,
		Endpoint:     endpoint,
	}
	return &GoogleOAuthConfig{oauthConfig: conf}
}

// IsConfigured returns true if client credentials are set.
func (c *GoogleOAuthConfig) IsConfigured() bool {
	return c.oauthConfig.ClientID != "" && c.oauthConfig.ClientSecret != ""
}

// AuthCodeURL produces the authorization URL directing the user to Google's consent screen.
// offline access ensures Google returns a refresh token.
func (c *GoogleOAuthConfig) AuthCodeURL(state string) string {
	return c.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
}

// ExchangeCode exchanges an authorization code for access and refresh tokens.
func (c *GoogleOAuthConfig) ExchangeCode(ctx context.Context, code string) (*oauth2.Token, error) {
	if !c.IsConfigured() {
		return nil, ErrOAuthNotConfigured
	}
	tok, err := c.oauthConfig.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code for token: %w", err)
	}
	return tok, nil
}

// TokenSource returns an auto-refreshing TokenSource for a given token.
func (c *GoogleOAuthConfig) TokenSource(ctx context.Context, token *oauth2.Token) oauth2.TokenSource {
	return c.oauthConfig.TokenSource(ctx, token)
}

// Client creates an http.Client authenticated via the token and refreshed automatically.
func (c *GoogleOAuthConfig) Client(ctx context.Context, token *oauth2.Token) *http.Client {
	return c.oauthConfig.Client(ctx, token)
}

// OAuthService manages OAuth token persistence and client creation.
type OAuthService struct {
	oauthConfig *GoogleOAuthConfig
	tokenRepo   database.OAuthTokenRepository
}

// NewOAuthService constructs an OAuthService.
func NewOAuthService(config *GoogleOAuthConfig, tokenRepo database.OAuthTokenRepository) *OAuthService {
	return &OAuthService{
		oauthConfig: config,
		tokenRepo:   tokenRepo,
	}
}

// GetGoogleConfig returns the underlying GoogleOAuthConfig.
func (s *OAuthService) GetGoogleConfig() *GoogleOAuthConfig {
	return s.oauthConfig
}

// SaveUserToken persists an OAuth token for a user.
func (s *OAuthService) SaveUserToken(ctx context.Context, userID, provider string, tok *oauth2.Token) error {
	if userID == "" {
		return errors.New("user ID required to save token")
	}
	record := &database.OAuthTokenRecord{
		UserID:       userID,
		Provider:     provider,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		TokenType:    tok.TokenType,
		Expiry:       tok.Expiry,
	}
	return s.tokenRepo.SaveToken(ctx, record)
}

// GetUserToken loads the stored OAuth token for a user.
func (s *OAuthService) GetUserToken(ctx context.Context, userID, provider string) (*oauth2.Token, error) {
	record, err := s.tokenRepo.GetToken(ctx, userID, provider)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, ErrOAuthTokenNotFound
		}
		return nil, err
	}

	tok := &oauth2.Token{
		AccessToken:  record.AccessToken,
		RefreshToken: record.RefreshToken,
		TokenType:    record.TokenType,
		Expiry:       record.Expiry,
	}
	return tok, nil
}

// DeleteUserToken deletes a user's stored OAuth token.
func (s *OAuthService) DeleteUserToken(ctx context.Context, userID, provider string) error {
	return s.tokenRepo.DeleteToken(ctx, userID, provider)
}

// GetAuthenticatedClient creates an http.Client with automatic token refresh.
func (s *OAuthService) GetAuthenticatedClient(ctx context.Context, userID, provider string) (*http.Client, error) {
	tok, err := s.GetUserToken(ctx, userID, provider)
	if err != nil {
		return nil, err
	}

	// Create TokenSource that will refresh if expired and persist new tokens
	ts := s.oauthConfig.TokenSource(ctx, tok)
	return oauth2.NewClient(ctx, &persistingTokenSource{
		base:     ts,
		service:  s,
		userID:   userID,
		provider: provider,
	}), nil
}

// persistingTokenSource wraps oauth2.TokenSource to update the database whenever a token refreshes.
type persistingTokenSource struct {
	base     oauth2.TokenSource
	service  *OAuthService
	userID   string
	provider string
}

func (s *persistingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := s.base.Token()
	if err != nil {
		return nil, err
	}
	// Best-effort asynchronous update of refreshed token in DB
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.service.SaveUserToken(ctx, s.userID, s.provider, tok)
	}()
	return tok, nil
}

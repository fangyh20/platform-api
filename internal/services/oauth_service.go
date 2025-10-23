package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/rapidbuildapp/rapidbuild/config"
	"github.com/rapidbuildapp/rapidbuild/internal/db"
	"github.com/rapidbuildapp/rapidbuild/internal/models"
)

type OAuthService struct {
	DB          *db.PostgresClient
	Config      *config.Config
	AuthService *AuthService
	googleConfig *oauth2.Config
}

type GoogleUser struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

func NewOAuthService(dbClient *db.PostgresClient, cfg *config.Config, authService *AuthService) *OAuthService {
	googleConfig := &oauth2.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  cfg.GoogleRedirectURL,
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}

	return &OAuthService{
		DB:           dbClient,
		Config:       cfg,
		AuthService:  authService,
		googleConfig: googleConfig,
	}
}

// GetGoogleAuthURL generates the OAuth URL for Google sign-in
func (s *OAuthService) GetGoogleAuthURL(state string) string {
	return s.googleConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// HandleGoogleCallback processes the OAuth callback from Google
func (s *OAuthService) HandleGoogleCallback(ctx context.Context, code string) (string, string, *models.User, error) {
	// Exchange code for token
	token, err := s.googleConfig.Exchange(ctx, code)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	// Get user info from Google
	googleUser, err := s.getGoogleUserInfo(ctx, token.AccessToken)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to get user info: %w", err)
	}

	// Check if user exists by Google ID
	user, err := s.getUserByGoogleID(ctx, googleUser.ID)
	if err == nil {
		// User exists, generate tokens and return
		accessToken, err := s.AuthService.GenerateAccessToken(user.ID, user.Email)
		if err != nil {
			return "", "", nil, err
		}

		refreshToken, err := s.AuthService.GenerateRefreshToken(user.ID)
		if err != nil {
			return "", "", nil, err
		}

		return accessToken, refreshToken, user, nil
	}

	// Check if user exists by email
	user, err = s.getUserByEmail(ctx, googleUser.Email)
	if err == nil {
		// Link Google account to existing user
		err = s.linkGoogleAccount(ctx, user.ID, googleUser.ID, googleUser.Picture)
		if err != nil {
			return "", "", nil, fmt.Errorf("failed to link Google account: %w", err)
		}

		user.GoogleID = &googleUser.ID
		user.AvatarURL = &googleUser.Picture

		accessToken, err := s.AuthService.GenerateAccessToken(user.ID, user.Email)
		if err != nil {
			return "", "", nil, err
		}

		refreshToken, err := s.AuthService.GenerateRefreshToken(user.ID)
		if err != nil {
			return "", "", nil, err
		}

		return accessToken, refreshToken, user, nil
	}

	// Create new user from Google
	user, err = s.createUserFromGoogle(ctx, googleUser)
	if err != nil {
		return "", "", nil, err
	}

	accessToken, err := s.AuthService.GenerateAccessToken(user.ID, user.Email)
	if err != nil {
		return "", "", nil, err
	}

	refreshToken, err := s.AuthService.GenerateRefreshToken(user.ID)
	if err != nil {
		return "", "", nil, err
	}

	return accessToken, refreshToken, user, nil
}

// getGoogleUserInfo fetches user information from Google
func (s *OAuthService) getGoogleUserInfo(ctx context.Context, accessToken string) (*GoogleUser, error) {
	url := "https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + accessToken

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("google API error: %s", string(body))
	}

	var googleUser GoogleUser
	if err := json.Unmarshal(body, &googleUser); err != nil {
		return nil, err
	}

	return &googleUser, nil
}

// getUserByGoogleID finds a user by their Google ID
func (s *OAuthService) getUserByGoogleID(ctx context.Context, googleID string) (*models.User, error) {
	user := &models.User{}
	query := `
		SELECT id, email, password_hash, full_name, avatar_url, email_verified, google_id, created_at, updated_at
		FROM users
		WHERE google_id = $1
	`
	err := s.DB.QueryRow(ctx, query, googleID).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.FullName, &user.AvatarURL,
		&user.EmailVerified, &user.GoogleID, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, errors.New("user not found")
	}

	return user, nil
}

// getUserByEmail finds a user by their email
func (s *OAuthService) getUserByEmail(ctx context.Context, email string) (*models.User, error) {
	user := &models.User{}
	query := `
		SELECT id, email, password_hash, full_name, avatar_url, email_verified, google_id, created_at, updated_at
		FROM users
		WHERE email = $1
	`
	err := s.DB.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.FullName, &user.AvatarURL,
		&user.EmailVerified, &user.GoogleID, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, errors.New("user not found")
	}

	return user, nil
}

// linkGoogleAccount links a Google account to an existing user
func (s *OAuthService) linkGoogleAccount(ctx context.Context, userID, googleID, avatarURL string) error {
	query := `
		UPDATE users
		SET google_id = $1, avatar_url = $2, email_verified = true, updated_at = $3
		WHERE id = $4
	`
	_, err := s.DB.Exec(ctx, query, googleID, avatarURL, time.Now(), userID)
	return err
}

// createUserFromGoogle creates a new user from Google profile
func (s *OAuthService) createUserFromGoogle(ctx context.Context, googleUser *GoogleUser) (*models.User, error) {
	user := &models.User{
		ID:            uuid.New().String(),
		Email:         googleUser.Email,
		FullName:      googleUser.Name,
		AvatarURL:     &googleUser.Picture,
		EmailVerified: googleUser.VerifiedEmail,
		GoogleID:      &googleUser.ID,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	query := `
		INSERT INTO users (id, email, full_name, avatar_url, email_verified, google_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := s.DB.Exec(ctx, query, user.ID, user.Email, user.FullName, user.AvatarURL, user.EmailVerified, user.GoogleID, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}

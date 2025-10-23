package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/rapidbuildapp/rapidbuild/config"
	"github.com/rapidbuildapp/rapidbuild/internal/db"
	"github.com/rapidbuildapp/rapidbuild/internal/models"
)

const (
	bcryptCost         = 12
	verificationExpiry = 24 * time.Hour
	resetTokenExpiry   = 1 * time.Hour
)

type AuthService struct {
	DB           *db.PostgresClient
	Config       *config.Config
	EmailService *EmailService
}

func NewAuthService(dbClient *db.PostgresClient, cfg *config.Config, emailService *EmailService) *AuthService {
	return &AuthService{
		DB:           dbClient,
		Config:       cfg,
		EmailService: emailService,
	}
}

// Signup creates a new user account and sends verification email
func (s *AuthService) Signup(ctx context.Context, email, password, fullName string) (*models.User, error) {
	// Check if user already exists
	var existingID string
	err := s.DB.QueryRow(ctx, "SELECT id FROM users WHERE email = $1", email).Scan(&existingID)
	if err == nil {
		return nil, errors.New("user with this email already exists")
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}
	hashedPasswordStr := string(hashedPassword)

	// Create user
	user := &models.User{
		ID:            uuid.New().String(),
		Email:         email,
		PasswordHash:  &hashedPasswordStr,
		FullName:      fullName,
		EmailVerified: false,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	query := `
		INSERT INTO users (id, email, password_hash, full_name, email_verified, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = s.DB.Exec(ctx, query, user.ID, user.Email, user.PasswordHash, user.FullName, user.EmailVerified, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Generate verification token
	token, err := generateSecureToken(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate verification token: %w", err)
	}

	// Store verification token
	expiresAt := time.Now().Add(verificationExpiry)
	query = `
		INSERT INTO email_verifications (user_id, token, expires_at)
		VALUES ($1, $2, $3)
	`
	_, err = s.DB.Exec(ctx, query, user.ID, token, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed to store verification token: %w", err)
	}

	// Send verification email
	go s.EmailService.SendVerificationEmail(user.Email, user.FullName, token)

	return user, nil
}

// Login authenticates a user and returns JWT token
func (s *AuthService) Login(ctx context.Context, email, password string) (string, string, *models.User, error) {
	// Get user
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
		return "", "", nil, errors.New("invalid email or password")
	}

	// Check if email is verified
	if !user.EmailVerified {
		return "", "", nil, errors.New("please verify your email before logging in")
	}

	// Check password (skip for Google-only accounts)
	if user.PasswordHash != nil && *user.PasswordHash != "" {
		err = bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(password))
		if err != nil {
			return "", "", nil, errors.New("invalid email or password")
		}
	} else {
		return "", "", nil, errors.New("this account uses Google sign-in")
	}

	// Generate tokens
	accessToken, err := s.GenerateAccessToken(user.ID, user.Email)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err := s.GenerateRefreshToken(user.ID)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	return accessToken, refreshToken, user, nil
}

// VerifyEmail confirms user's email address
func (s *AuthService) VerifyEmail(ctx context.Context, token string) error {
	// Find verification record
	var userID string
	var expiresAt time.Time
	query := `
		SELECT user_id, expires_at
		FROM email_verifications
		WHERE token = $1
	`
	err := s.DB.QueryRow(ctx, query, token).Scan(&userID, &expiresAt)
	if err != nil {
		return errors.New("invalid or expired verification token")
	}

	// Check if expired
	if time.Now().After(expiresAt) {
		return errors.New("verification token has expired")
	}

	// Update user as verified
	query = `UPDATE users SET email_verified = true, updated_at = $1 WHERE id = $2`
	_, err = s.DB.Exec(ctx, query, time.Now(), userID)
	if err != nil {
		return fmt.Errorf("failed to verify email: %w", err)
	}

	// Delete used verification token
	query = `DELETE FROM email_verifications WHERE token = $1`
	_, err = s.DB.Exec(ctx, query, token)

	return err
}

// ForgotPassword initiates password reset process
func (s *AuthService) ForgotPassword(ctx context.Context, email string) error {
	// Find user
	var userID, fullName string
	query := `SELECT id, full_name FROM users WHERE email = $1`
	err := s.DB.QueryRow(ctx, query, email).Scan(&userID, &fullName)
	if err != nil {
		// Don't reveal if email exists
		return nil
	}

	// Generate reset token
	token, err := generateSecureToken(32)
	if err != nil {
		return fmt.Errorf("failed to generate reset token: %w", err)
	}

	// Store reset token
	expiresAt := time.Now().Add(resetTokenExpiry)
	query = `
		INSERT INTO password_resets (user_id, token, expires_at)
		VALUES ($1, $2, $3)
	`
	_, err = s.DB.Exec(ctx, query, userID, token, expiresAt)
	if err != nil {
		return fmt.Errorf("failed to store reset token: %w", err)
	}

	// Send reset email
	go s.EmailService.SendPasswordResetEmail(email, fullName, token)

	return nil
}

// ResetPassword resets user's password with token
func (s *AuthService) ResetPassword(ctx context.Context, token, newPassword string) error {
	// Find reset record
	var userID string
	var expiresAt time.Time
	var used bool
	query := `
		SELECT user_id, expires_at, used
		FROM password_resets
		WHERE token = $1
	`
	err := s.DB.QueryRow(ctx, query, token).Scan(&userID, &expiresAt, &used)
	if err != nil {
		return errors.New("invalid or expired reset token")
	}

	// Check if expired or already used
	if time.Now().After(expiresAt) || used {
		return errors.New("reset token has expired or been used")
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Update password
	query = `UPDATE users SET password_hash = $1, updated_at = $2 WHERE id = $3`
	_, err = s.DB.Exec(ctx, query, string(hashedPassword), time.Now(), userID)
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	// Mark token as used
	query = `UPDATE password_resets SET used = true WHERE token = $1`
	_, err = s.DB.Exec(ctx, query, token)

	return err
}

// GenerateAccessToken creates a JWT access token
func (s *AuthService) GenerateAccessToken(userID, email string) (string, error) {
	claims := jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"exp":   time.Now().Add(s.Config.JWTExpiry).Unix(),
		"iat":   time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.Config.JWTSecret))
}

// GenerateRefreshToken creates a refresh token
func (s *AuthService) GenerateRefreshToken(userID string) (string, error) {
	claims := jwt.MapClaims{
		"sub":  userID,
		"type": "refresh",
		"exp":  time.Now().Add(s.Config.RefreshTokenExpiry).Unix(),
		"iat":  time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.Config.JWTSecret))
}

// RefreshAccessToken generates new access token from refresh token
func (s *AuthService) RefreshAccessToken(ctx context.Context, refreshToken string) (string, error) {
	// Parse and validate refresh token
	token, err := jwt.Parse(refreshToken, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(s.Config.JWTSecret), nil
	})

	if err != nil || !token.Valid {
		return "", errors.New("invalid refresh token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid token claims")
	}

	// Check if it's a refresh token
	if tokenType, ok := claims["type"].(string); !ok || tokenType != "refresh" {
		return "", errors.New("not a refresh token")
	}

	userID := claims["sub"].(string)

	// Get user email
	var email string
	query := `SELECT email FROM users WHERE id = $1`
	err = s.DB.QueryRow(ctx, query, userID).Scan(&email)
	if err != nil {
		return "", errors.New("user not found")
	}

	// Generate new access token
	return s.GenerateAccessToken(userID, email)
}

// GetUserByID retrieves a user by ID
func (s *AuthService) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	user := &models.User{}
	query := `
		SELECT id, email, password_hash, full_name, avatar_url, email_verified, google_id, created_at, updated_at
		FROM users
		WHERE id = $1
	`
	err := s.DB.QueryRow(ctx, query, userID).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.FullName, &user.AvatarURL,
		&user.EmailVerified, &user.GoogleID, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, errors.New("user not found")
	}

	return user, nil
}

// Helper function to generate secure random tokens
func generateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

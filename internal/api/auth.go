package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/rapidbuildapp/rapidbuild/config"
	"github.com/rapidbuildapp/rapidbuild/internal/middleware"
	"github.com/rapidbuildapp/rapidbuild/internal/services"
)

type AuthHandler struct {
	AuthService  *services.AuthService
	OAuthService *services.OAuthService
	Config       *config.Config
}

func NewAuthHandler(authService *services.AuthService, oauthService *services.OAuthService, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		AuthService:  authService,
		Config:       cfg,
		OAuthService: oauthService,
	}
}

// SignupRequest represents the signup request
type SignupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
}

// LoginRequest represents the login request
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// ForgotPasswordRequest represents the forgot password request
type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

// ResetPasswordRequest represents the reset password request
type ResetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// AuthResponse represents the authentication response
type AuthResponse struct {
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	User         interface{} `json:"user"`
}

// Signup handles POST /auth/signup
func (h *AuthHandler) Signup(w http.ResponseWriter, r *http.Request) {
	var req SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate input
	if req.Email == "" || req.Password == "" || req.FullName == "" {
		middleware.RespondError(w, http.StatusBadRequest, "Email, password, and full name are required")
		return
	}

	if len(req.Password) < 8 {
		middleware.RespondError(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}

	user, err := h.AuthService.Signup(r.Context(), req.Email, req.Password, req.FullName)
	if err != nil {
		middleware.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	middleware.RespondJSON(w, http.StatusCreated, map[string]interface{}{
		"message": "Account created successfully. Please check your email to verify your account.",
		"user": map[string]interface{}{
			"id":    user.ID,
			"email": user.Email,
		},
	})
}

// Login handles POST /auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		middleware.RespondError(w, http.StatusBadRequest, "Email and password are required")
		return
	}

	accessToken, refreshToken, user, err := h.AuthService.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		middleware.RespondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	middleware.RespondJSON(w, http.StatusOK, AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User:         user,
	})
}

// VerifyEmail handles GET /auth/verify-email?token=xxx
func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		middleware.RespondError(w, http.StatusBadRequest, "Token is required")
		return
	}

	err := h.AuthService.VerifyEmail(r.Context(), token)
	if err != nil {
		middleware.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	middleware.RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Email verified successfully. You can now log in.",
	})
}

// ForgotPassword handles POST /auth/forgot-password
func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Email == "" {
		middleware.RespondError(w, http.StatusBadRequest, "Email is required")
		return
	}

	// Always return success to prevent email enumeration
	_ = h.AuthService.ForgotPassword(r.Context(), req.Email)

	middleware.RespondJSON(w, http.StatusOK, map[string]string{
		"message": "If an account exists with this email, a password reset link has been sent.",
	})
}

// ResetPassword handles POST /auth/reset-password
func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Token == "" || req.NewPassword == "" {
		middleware.RespondError(w, http.StatusBadRequest, "Token and new password are required")
		return
	}

	if len(req.NewPassword) < 8 {
		middleware.RespondError(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}

	err := h.AuthService.ResetPassword(r.Context(), req.Token, req.NewPassword)
	if err != nil {
		middleware.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	middleware.RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Password reset successfully. You can now log in with your new password.",
	})
}

// GoogleAuth handles GET /auth/google
func (h *AuthHandler) GoogleAuth(w http.ResponseWriter, r *http.Request) {
	// Generate state token for CSRF protection
	state, err := generateStateToken()
	if err != nil {
		middleware.RespondError(w, http.StatusInternalServerError, "Failed to generate state token")
		return
	}

	// Store state in cookie (or session)
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600, // 10 minutes
		Path:     "/",
	})

	url := h.OAuthService.GetGoogleAuthURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// GoogleCallback handles GET /auth/google/callback
func (h *AuthHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	// Verify state parameter
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil {
		middleware.RespondError(w, http.StatusBadRequest, "Invalid state cookie")
		return
	}

	state := r.URL.Query().Get("state")
	if state != stateCookie.Value {
		middleware.RespondError(w, http.StatusBadRequest, "Invalid state parameter")
		return
	}

	// Get authorization code
	code := r.URL.Query().Get("code")
	if code == "" {
		middleware.RespondError(w, http.StatusBadRequest, "Authorization code not found")
		return
	}

	// Exchange code for tokens and user info
	accessToken, refreshToken, user, err := h.OAuthService.HandleGoogleCallback(r.Context(), code)
	if err != nil {
		// Redirect to frontend with error
		redirectURL := h.Config.FrontendURL + "/auth/google/callback?error=" + url.QueryEscape(err.Error())
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
		return
	}

	// Redirect to frontend with tokens (they will be in URL params, frontend should move them to localStorage immediately)
	// Using fragment (#) instead of query params for better security (not sent to server)
	redirectURL := fmt.Sprintf("%s/auth/google/callback#access_token=%s&refresh_token=%s&user_id=%s&email=%s&full_name=%s",
		h.Config.FrontendURL,
		url.QueryEscape(accessToken),
		url.QueryEscape(refreshToken),
		url.QueryEscape(user.ID),
		url.QueryEscape(user.Email),
		url.QueryEscape(user.FullName),
	)
	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

// RefreshToken handles POST /auth/refresh
func (h *AuthHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.RefreshToken == "" {
		middleware.RespondError(w, http.StatusBadRequest, "Refresh token is required")
		return
	}

	accessToken, err := h.AuthService.RefreshAccessToken(r.Context(), req.RefreshToken)
	if err != nil {
		middleware.RespondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	middleware.RespondJSON(w, http.StatusOK, map[string]string{
		"access_token": accessToken,
	})
}

// GetCurrentUser handles GET /auth/me
func (h *AuthHandler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	userClaims, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		middleware.RespondError(w, http.StatusUnauthorized, "User not found in context")
		return
	}

	user, err := h.AuthService.GetUserByID(r.Context(), userClaims.Sub)
	if err != nil {
		middleware.RespondError(w, http.StatusNotFound, "User not found")
		return
	}

	middleware.RespondJSON(w, http.StatusOK, user)
}

// Logout handles POST /auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// In a stateless JWT system, logout is handled client-side
	// But we can clear cookies if using them
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    "",
		HttpOnly: true,
		MaxAge:   -1,
		Path:     "/",
	})

	middleware.RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Logged out successfully",
	})
}

// Helper function to generate state token for OAuth
func generateStateToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

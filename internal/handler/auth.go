package handler

import (
	"encoding/json"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/auth"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/response"
	"github.com/mcchukwu/egentop/internal/validation"
	"github.com/mcchukwu/egentop/pkg/config"
)

var cfg = config.Load()

type AuthHandler struct {
	AuthService *auth.AuthService
}

func NewAuthHandler(authService *auth.AuthService) *AuthHandler {
	return &AuthHandler{AuthService: authService}
}

// Register creates a new user account
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	// Decode request
	var req auth.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	// Validate request
	validationErrors := validation.ValidateRegisterRequest(req)
	if validationErrors.HasErrors() {
		response.ValidationError(w, validationErrors)
		return
	}

	// Call register service
	if err := h.AuthService.Register(r.Context(), req); err != nil {
		response.HandleError(w, err)
		return
	}

	// Return response
	response.Created(w, map[string]string{
		"message": "user created",
	})
}

// Login validates the user credentials and returns a JWT access token
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	// Decode request
	var req auth.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	// Validate request
	validationErrors := validation.ValidateLoginRequest(req)
	if validationErrors.HasErrors() {
		response.ValidationError(w, validationErrors)
		return
	}

	// Call login service
	accessToken, refreshToken, err := h.AuthService.Login(r.Context(), req)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		HttpOnly: true,
		Secure:   cfg.AppEnv == "production", // true in production HTTPS
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 30,
	})

	// Return response
	response.OK(w, map[string]any{
		"access_token": accessToken,
	})
}

// RefreshToken refreshes the session
func (h *AuthHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	// Read cookie
	cookie, err := r.Cookie("refresh_token")
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidToken)
		return
	}

	accessToken, newRefreshToken, err := h.AuthService.RefreshToken(r.Context(), cookie.Value)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	// Set new cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    newRefreshToken,
		HttpOnly: true,
		Secure:   cfg.AppEnv == "production", // true in production HTTPS
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 30,
	})

	// Return new access token
	response.OK(w, map[string]any{
		"access_token": accessToken,
	})
}

// Logout invalidates the session
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// Get session id
	sessionID, ok := r.Context().Value(middleware.SessionIDKey).(string)
	if !ok {
		response.HandleError(w, apperrors.ErrExpiredToken)
		return
	}

	// Call logout service
	if err := h.AuthService.Logout(r.Context(), sessionID); err != nil {
		response.HandleError(w, err)
		return
	}

	// Delete refresh token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.AppEnv == "production", // true in production HTTPS
		MaxAge:   -1,
	})

	// Return response
	response.NoContent(w)
}

// LogoutAllDevices revokes all sessions for a user
func (h *AuthHandler) LogoutAllDevices(w http.ResponseWriter, r *http.Request) {
	// Find user
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		response.HandleError(w, apperrors.ErrUserNotFound)
		return
	}

	// Call logoutalldevices service
	if err := h.AuthService.LogoutAllDevices(r.Context(), userID); err != nil {
		response.HandleError(w, err)
		return
	}

	// Revoke refresh token
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.AppEnv == "production",
		MaxAge:   -1,
	})

	// Return response
	response.NoContent(w)
}

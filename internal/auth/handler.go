package auth

import (
	"encoding/json"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/normalize"
	"github.com/mcchukwu/egentop/internal/response"
	"github.com/mcchukwu/egentop/internal/validation"
	"github.com/mcchukwu/egentop/pkg/config"
)

type Handler struct {
	Service *Service
	Config  *config.Config
}

func NewHandler(service *Service, cfg *config.Config) *Handler {
	return &Handler{Service: service, Config: cfg}
}

// Register creates a new user account
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest

	// Decode request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	// Normalize phone number
	if req.Phone != "" {
		normalized, err := normalize.Phone(req.Phone, "")
		if err != nil {
			response.ValidationError(w, map[string]string{"phone": "must be a valid phone number"})
			return
		}
		req.Phone = normalized
	}

	// Normalize email
	if req.Email != "" {
		req.Email = normalize.Email(req.Email)
	}

	// Validate request
	if err := validation.ValidateStruct(req); err != nil {
		response.ValidationError(w, err)
		return
	}

	// Call register service
	accessToken, refreshToken, err := h.Service.Register(r.Context(), req)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		HttpOnly: true,
		Secure:   h.Config.AppEnv == "production", // true in production HTTPS
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 30,
	})

	// Return response
	response.Success(w, http.StatusOK, "registration successful", map[string]any{
		"access_token": accessToken,
	})
}

// Login validates the user credentials and returns a JWT access token
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	// Normalize if identifier is a phone number
	req.Identifier = normalize.Identifier(req.Identifier, "")

	// Validate request
	if err := validation.ValidateStruct(req); err != nil {
		response.ValidationError(w, err)
		return
	}

	// Call login service
	accessToken, refreshToken, err := h.Service.Login(r.Context(), req)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		HttpOnly: true,
		Secure:   h.Config.AppEnv == "production", // true in production HTTPS
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 30,
	})

	// Return response
	response.Success(w, http.StatusOK, "login successful", map[string]any{
		"access_token": accessToken,
	})
}

// RefreshToken refreshes the session
func (h *Handler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	// Read cookie
	cookie, err := r.Cookie("refresh_token")
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidToken)
		return
	}

	accessToken, newRefreshToken, err := h.Service.RefreshToken(r.Context(), cookie.Value)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	// Set new cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    newRefreshToken,
		HttpOnly: true,
		Secure:   h.Config.AppEnv == "production", // true in production HTTPS
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 30,
	})

	// Return new access token
	response.Success(w, http.StatusOK, "login successful", map[string]any{
		"access_token": accessToken,
	})
}

// Logout invalidates the session
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	// Cookie-only auth: any error (missing or malformed cookie) is treated as
	// already logged out — 204 and clear, never 401.
	cookie, err := r.Cookie("refresh_token")
	if err != nil {
		h.clearRefreshCookie(w)
		response.Success(w, http.StatusNoContent, "logout successful", nil)
		return
	}

	if err := h.Service.Logout(r.Context(), cookie.Value); err != nil {
		response.HandleError(w, err) // only genuine DB/audit failures (500)
		return
	}

	h.clearRefreshCookie(w)
	response.Success(w, http.StatusNoContent, "logout successful", nil)
}

// LogoutAllDevices revokes all sessions for a user
func (h *Handler) LogoutAllDevices(w http.ResponseWriter, r *http.Request) {
	// Cookie-only auth: any error (missing or malformed cookie) is treated as
	// already logged out — 204 and clear, never 401.
	cookie, err := r.Cookie("refresh_token")
	if err != nil {
		h.clearRefreshCookie(w)
		response.Success(w, http.StatusNoContent, "logout successful", nil)
		return
	}

	if err := h.Service.LogoutAllDevices(r.Context(), cookie.Value); err != nil {
		response.HandleError(w, err) // only genuine DB/audit failures (500)
		return
	}

	h.clearRefreshCookie(w)
	response.Success(w, http.StatusNoContent, "logout successful", nil)
}

// clearRefreshCookie deletes the refresh_token cookie (attributes preserved
// verbatim from the previous implementation).
func (h *Handler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.Config.AppEnv == "production", // true in production HTTPS
		MaxAge:   -1,
	})
}

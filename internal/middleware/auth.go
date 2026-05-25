package middleware

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
)

type AccessTokenClaims struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`

	jwt.RegisteredClaims
}

type AuthMiddleware struct {
	DB        *sql.DB
	JWTSecret []byte
}

func NewAuthMiddleware(db *sql.DB, secret []byte) *AuthMiddleware {
	return &AuthMiddleware{
		DB:        db,
		JWTSecret: secret,
	}
}

func (m *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")

		if authHeader == "" {
			response.HandleError(w, apperrors.ErrUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")

		if len(parts) != 2 || parts[0] != "Bearer" {
			response.HandleError(w, apperrors.ErrInvalidToken)
			return
		}

		tokenString := parts[1]

		claims := &AccessTokenClaims{}

		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
			// enforce HMAC signing only
			_, ok := token.Method.(*jwt.SigningMethodHMAC)
			if !ok {
				return nil, apperrors.ErrInvalidToken
			}

			return m.JWTSecret, nil
		})

		if err != nil || !token.Valid {
			response.HandleError(w, apperrors.ErrUnauthorized)
			return
		}

		if claims.UserID == "" || claims.SessionID == "" {
			response.HandleError(w, apperrors.ErrUnauthorized)
			return
		}

		// validate active session
		var exists bool

		err = m.DB.QueryRowContext(r.Context(),
			`
				SELECT EXISTS (
				SELECT 1
				FROM sessions
				WHERE id = $1
				  AND user_id = $2
				  AND revoked = false
				  AND expires_at > NOW()
			)`,
			claims.SessionID, claims.UserID).Scan(&exists)
		if err != nil {
			response.HandleError(w, err)
			return
		}

		if !exists {
			response.HandleError(w, apperrors.ErrSessionExpired)
			return
		}

		// attach auth context
		ctx := requestctx.WithUserID(r.Context(), claims.UserID)
		ctx = requestctx.WithSessionID(ctx, claims.SessionID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

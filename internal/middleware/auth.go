package middleware

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
)

type AccessTokenClaims struct {
	UserID    uuid.UUID `json:"user_id"`
	SessionID uuid.UUID `json:"session_id"`

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

		if claims.UserID == uuid.Nil || claims.SessionID == uuid.Nil {
			response.HandleError(w, apperrors.ErrUnauthorized)
			return
		}

		// validate active session and load the forced-password-change flag
		var mustChangePassword bool

		err = m.DB.QueryRowContext(r.Context(),
			`
				SELECT u.must_change_password
				FROM sessions s
				JOIN users u ON u.id = s.user_id
				WHERE s.id = $1
				  AND s.user_id = $2
				  AND s.revoked = false
				  AND s.expires_at > NOW()
			`,
			claims.SessionID, claims.UserID).Scan(&mustChangePassword)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.HandleError(w, apperrors.ErrSessionExpired)
				return
			}
			response.HandleError(w, apperrors.ErrDatabase)
			return
		}

		// attach auth context
		ctx := requestctx.WithUserID(r.Context(), claims.UserID)
		ctx = requestctx.WithSessionID(ctx, claims.SessionID)
		ctx = requestctx.WithMustChangePassword(ctx, mustChangePassword)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

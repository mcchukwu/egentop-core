package middleware

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
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
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")

			if authHeader == "" {
				unauthorized(w)
				return
			}

			parts := strings.Split(authHeader, " ")

			if len(parts) != 2 || parts[0] != "Bearer" {
				unauthorized(w)
				return
			}

			tokenString := parts[1]

			claims := &AccessTokenClaims{}

			token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
				// enforce HMAC signing only
				_, ok := token.Method.(*jwt.SigningMethodHMAC)
				if !ok {
					return nil, errors.New(
						"invalid signing method",
					)
				}

				return m.JWTSecret, nil
			})

			if err != nil || !token.Valid {
				unauthorized(w)
				return
			}

			if claims.UserID == "" || claims.SessionID == "" {
				unauthorized(w)
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
				claims.SessionID,
				claims.UserID,
			).Scan(&exists)

			if err != nil {
				response.HandleError(w, err)
				return
			}

			if !exists {
				unauthorized(w)
				return
			}

			// attach auth context
			ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, SessionIDKey, claims.SessionID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
}

// Helpers
func unauthorized(w http.ResponseWriter) {
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

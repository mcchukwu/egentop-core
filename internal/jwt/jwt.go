package jwt

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Manager struct {
	secret    []byte
	accessTTL time.Duration
}

func NewManager(secret string, accessTTL time.Duration) *Manager {
	return &Manager{
		secret:    []byte(secret),
		accessTTL: accessTTL,
	}
}

func (m *Manager) GenerateAccessToken(userID, sessionID uuid.UUID) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":    userID,
		"session_id": sessionID,
		"exp":        time.Now().Add(m.accessTTL).Unix(),
	})

	return token.SignedString(m.secret)
}

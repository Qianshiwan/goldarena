package jwt

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	Nickname    string `json:"nickname"`
	TokenType   string `json:"token_type"` // access | refresh
	jwt.RegisteredClaims
}

type Manager struct {
	secret        []byte
	accessExpire  time.Duration
	refreshExpire time.Duration
}

func NewManager(secret string, accessExpire, refreshExpire time.Duration) *Manager {
	return &Manager{
		secret:        []byte(secret),
		accessExpire:  accessExpire,
		refreshExpire: refreshExpire,
	}
}

func (m *Manager) GenerateAccessToken(userID int64, username, nickname string) (string, error) {
	return m.generate(userID, username, nickname, "access", m.accessExpire)
}

func (m *Manager) GenerateRefreshToken(userID int64, username, nickname string) (string, error) {
	return m.generate(userID, username, nickname, "refresh", m.refreshExpire)
}

func (m *Manager) generate(userID int64, username, nickname, tokenType string, expire time.Duration) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:    userID,
		Username:  username,
		Nickname:  nickname,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expire)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

func (m *Manager) ParseToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, jwt.ErrSignatureInvalid
	}
	return claims, nil
}

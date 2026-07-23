package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims 业务令牌载荷
type Claims struct {
	UserID   uint64 `json:"uid"`
	Username string `json:"username"`
	IsSuper  bool   `json:"is_super"`
	jwt.RegisteredClaims
}

// TokenManager 负责 JWT 的签发与解析
type TokenManager struct {
	secret []byte
	issuer string
	expire time.Duration
}

func NewTokenManager(cfg Config) *TokenManager {
	expire := cfg.ExpireMinutes
	if expire <= 0 {
		expire = 120 // 默认 2 小时
	}
	return &TokenManager{
		secret: []byte(cfg.Secret),
		issuer: cfg.Issuer,
		expire: expire * time.Minute,
	}
}

// Generate 为用户签发令牌
func (m *TokenManager) Generate(userID uint64, username string, isSuper bool) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		Username: username,
		IsSuper:  isSuper,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.expire)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// Parse 解析并校验令牌，失败返回错误
func (m *TokenManager) Parse(tokenStr string) (*Claims, error) {
	if tokenStr == "" {
		return nil, errors.New("empty token")
	}
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

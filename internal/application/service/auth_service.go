package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
)

type AuthService struct {
	userRepo    repository.UserRepository
	sessionRepo repository.SessionRepository
	jwtSecret   string
	jwtExpiry   time.Duration
	refreshExpiry time.Duration
}

type AuthConfig struct {
	JWTSecret      string
	JWTExpiry      time.Duration // Access Token 过期时间，默认 15 分钟
	RefreshExpiry  time.Duration // Refresh Token 过期时间，默认 30 天
}

func NewAuthService(
	userRepo repository.UserRepository,
	sessionRepo repository.SessionRepository,
	cfg AuthConfig,
) *AuthService {
	// 默认 JWT token 过期时间：7天（更长的有效期，减少频繁登录）
	if cfg.JWTExpiry == 0 {
		cfg.JWTExpiry = 7 * 24 * time.Hour
	}
	if cfg.RefreshExpiry == 0 {
		cfg.RefreshExpiry = 30 * 24 * time.Hour
	}
	return &AuthService{
		userRepo:      userRepo,
		sessionRepo:   sessionRepo,
		jwtSecret:     cfg.JWTSecret,
		jwtExpiry:     cfg.JWTExpiry,
		refreshExpiry: cfg.RefreshExpiry,
	}
}

// LoginRequest 登录请求
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // 秒数
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RegisterResponse 注册响应
type RegisterResponse struct {
	UserID uint64 `json:"user_id"`
}

// Login 用户登录
func (s *AuthService) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	// 查找用户
	user, err := s.userRepo.FindByEmail(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	if user == nil {
		return nil, errors.New("invalid email or password")
	}

	// 验证密码
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		return nil, errors.New("invalid email or password")
	}

	// 生成 Access Token
	accessToken, err := s.generateAccessToken(user.ID)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	// 生成 Refresh Token
	refreshToken, err := s.generateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	// 保存 Session
	session := &entity.UserSession{
		UserID:       user.ID,
		RefreshToken: refreshToken,
		DeviceInfo:   "", // 可以从请求头获取
		ExpiresAt:    time.Now().Add(s.refreshExpiry),
	}
	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return &LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(s.jwtExpiry.Seconds()),
	}, nil
}

// Register 用户注册
func (s *AuthService) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	// 检查用户是否已存在
	existing, err := s.userRepo.FindByEmail(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	if existing != nil {
		return nil, errors.New("email already exists")
	}

	// 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	// 创建用户
	user := &entity.User{
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
		Salt:         "", // Bcrypt 不需要额外 salt
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	return &RegisterResponse{UserID: user.ID}, nil
}

// RefreshToken 刷新 Access Token
func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error) {
	// 查找 Session
	session, err := s.sessionRepo.FindByRefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, fmt.Errorf("find session: %w", err)
	}
	if session == nil {
		return nil, errors.New("invalid refresh token")
	}

	// 检查是否过期
	if time.Now().After(session.ExpiresAt) {
		return nil, errors.New("refresh token expired")
	}

	// 生成新的 Access Token
	accessToken, err := s.generateAccessToken(session.UserID)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	// 生成新的 Refresh Token（可选：刷新 refresh token）
	newRefreshToken, err := s.generateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	// 更新 Session
	session.RefreshToken = newRefreshToken
	session.ExpiresAt = time.Now().Add(s.refreshExpiry)
	if err := s.sessionRepo.Update(ctx, session); err != nil {
		return nil, fmt.Errorf("update session: %w", err)
	}

	return &LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		ExpiresIn:    int(s.jwtExpiry.Seconds()),
	}, nil
}

// ValidateToken 验证 Access Token 并返回 UserID
func (s *AuthService) ValidateToken(tokenString string) (uint64, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.jwtSecret), nil
	})

	if err != nil {
		return 0, fmt.Errorf("parse token: %w", err)
	}

	if !token.Valid {
		return 0, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, errors.New("invalid token claims")
	}

	userID, ok := claims["user_id"].(float64)
	if !ok {
		return 0, errors.New("invalid user_id in token")
	}

	return uint64(userID), nil
}

// generateAccessToken 生成 JWT Access Token
func (s *AuthService) generateAccessToken(userID uint64) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(s.jwtExpiry).Unix(),
		"iat":     time.Now().Unix(),
		"type":    "access",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}

// generateRefreshToken 生成随机 Refresh Token
func (s *AuthService) generateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

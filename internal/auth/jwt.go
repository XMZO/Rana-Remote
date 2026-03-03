package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID string `json:"uid"`
	Role   string `json:"role"`
	Locale string `json:"locale"`
	Type   string `json:"typ"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type TokenManager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewTokenManager(secret string, accessTTL, refreshTTL time.Duration) (*TokenManager, error) {
	if secret == "" {
		return nil, errors.New("jwt secret is required")
	}
	if accessTTL <= 0 || refreshTTL <= 0 {
		return nil, errors.New("token ttl must be > 0")
	}
	return &TokenManager{secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}, nil
}

func (m *TokenManager) Generate(userID, role, locale string, now time.Time) (TokenPair, error) {
	accessExp := now.Add(m.accessTTL)
	refreshExp := now.Add(m.refreshTTL)

	accessClaims := Claims{
		UserID: userID,
		Role:   role,
		Locale: locale,
		Type:   "access",
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(accessExp),
			Subject:   userID,
		},
	}
	refreshClaims := Claims{
		UserID: userID,
		Role:   role,
		Locale: locale,
		Type:   "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(refreshExp),
			Subject:   userID,
		},
	}

	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(m.secret)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(m.secret)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresAt: accessExp}, nil
}

func (m *TokenManager) Parse(token string, expectedType string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("invalid signing method")
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token claims")
	}
	if claims.Type != expectedType {
		return nil, errors.New("unexpected token type")
	}
	return claims, nil
}

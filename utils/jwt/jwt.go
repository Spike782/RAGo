package jwt

import (
	"ai-chat/config"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

type Claims struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// GenerateToken creates a signed JWT token.
func GenerateToken(id int64, email string) (string, error) {
	claims := Claims{
		ID:    id,
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(config.GetConfig().ExpireDuration) * time.Hour)),
			Issuer:    config.GetConfig().Issuer,
			Subject:   config.GetConfig().Subject,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	// Config uses a string secret, so HMAC is the correct algorithm.
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(config.GetConfig().Key))
}

// ParseToken parses and validates JWT token.
func ParseToken(token string) (string, bool) {
	claims := new(Claims)
	t, err := jwt.ParseWithClaims(
		token,
		claims,
		func(t *jwt.Token) (interface{}, error) {
			return []byte(config.GetConfig().Key), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil || !t.Valid || claims == nil {
		return "", false
	}
	if claims.Email == "" {
		return "", false
	}
	return claims.Email, true
}

package auth

import (
	"crypto/rand"
	"encoding/base64"
	"math/big"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/golang-jwt/jwt/v5"
)

var (
	jwtSecretOnce sync.Once
	jwtSecretKey  []byte
)

// getJWTSecret returns the JWT signing key, generating and persisting one if needed.
func getJWTSecret() []byte {
	jwtSecretOnce.Do(func() {
		secret, err := op.SettingGetString(model.SettingKeyJWTSecret)
		if err == nil && secret != "" {
			jwtSecretKey = []byte(secret)
			return
		}

		// Generate a random 32-byte secret
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			// Fallback to legacy method if random generation fails
			user := op.UserGet()
			jwtSecretKey = []byte(user.Username + user.Password)
			log.Warnf("failed to generate random JWT secret, using legacy method")
			return
		}
		generated := base64.RawURLEncoding.EncodeToString(b)
		if err := op.SettingSetString(model.SettingKeyJWTSecret, generated); err != nil {
			// If we can't persist, still use the generated key for this session
			log.Warnf("failed to persist JWT secret: %s", err.Error())
		}
		jwtSecretKey = []byte(generated)
	})
	return jwtSecretKey
}

func GenerateJWTToken(expiresMin int) (string, string, error) {
	now := time.Now()
	claims := &jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		Issuer:    conf.APP_NAME,
	}
	if expiresMin == 0 {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(time.Duration(15) * time.Minute))
	} else if expiresMin > 0 {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(time.Duration(expiresMin) * time.Minute))
	} else if expiresMin == -1 {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(time.Duration(30) * 24 * time.Hour))
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(getJWTSecret())
	if err != nil {
		return "", "", err
	}
	return token, claims.ExpiresAt.Format(time.RFC3339), nil
}

func VerifyJWTToken(token string) bool {
	jwtToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		return getJWTSecret(), nil
	})
	if err != nil || !jwtToken.Valid {
		return false
	}
	return true
}

func GenerateAPIKey() string {
	const keyChars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, 48)
	maxI := big.NewInt(int64(len(keyChars)))
	for i := range b {
		n, err := rand.Int(rand.Reader, maxI)
		if err != nil {
			return ""
		}
		b[i] = keyChars[n.Int64()]
	}
	return "sk-" + conf.APP_NAME + "-" + string(b)
}

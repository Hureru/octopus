package op

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/bestruirui/octopus/internal/model"
)

func EncryptSecret(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	aead, err := secretAEAD()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, []byte(value), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func DecryptSecret(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("decode encrypted secret: %w", err)
	}
	aead, err := secretAEAD()
	if err != nil {
		return "", err
	}
	if len(payload) < aead.NonceSize() {
		return "", fmt.Errorf("encrypted secret is truncated")
	}
	plain, err := aead.Open(nil, payload[:aead.NonceSize()], payload[aead.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plain), nil
}

func secretAEAD() (cipher.AEAD, error) {
	secret, err := SettingGetString(model.SettingKeyJWTSecret)
	if err != nil {
		return nil, err
	}
	return secretAEADForJWT(secret)
}

func secretAEADForJWT(secret string) (cipher.AEAD, error) {
	if secret == "" {
		return nil, fmt.Errorf("jwt secret is not initialized")
	}
	key := sha256.Sum256([]byte("octopus-secret-v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func reencryptSecret(value, sourceJWT, targetJWT string) (string, error) {
	if value == "" {
		return value, nil
	}
	sourceAEAD, err := secretAEADForJWT(sourceJWT)
	if err != nil {
		return "", err
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("decode encrypted secret: %w", err)
	}
	if len(payload) < sourceAEAD.NonceSize() {
		return "", fmt.Errorf("encrypted secret is truncated")
	}
	plain, err := sourceAEAD.Open(nil, payload[:sourceAEAD.NonceSize()], payload[sourceAEAD.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret with source jwt: %w", err)
	}
	if sourceJWT == targetJWT {
		return value, nil
	}
	targetAEAD, err := secretAEADForJWT(targetJWT)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, targetAEAD.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := targetAEAD.Seal(nonce, nonce, plain, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

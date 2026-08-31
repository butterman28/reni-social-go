// internal/utils/crypto.go
package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"reni-social/pkg/config"
	"strings"
)

const encryptionPrefix = "enc:v1:"

// EncryptToken encrypts a string token using AES-256-GCM.
// Returns a prefixed, base64-encoded string containing the nonce and ciphertext.
func EncryptToken(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	key := []byte(config.C.TokenEncryptionKey)
	if len(key) != 32 {
		return "", errors.New("TOKEN_ENCRYPTION_KEY must be exactly 32 bytes (256 bits)")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptionPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptToken decrypts a prefixed, base64-encoded AES-256-GCM ciphertext.
// If the string does not have the encryption prefix, it returns the original string.
// This ensures backward compatibility with legacy plain-text tokens during migration.
func DecryptToken(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}

	// If it doesn't have our prefix, assume it's a legacy plain-text token
	if !strings.HasPrefix(encoded, encryptionPrefix) {
		return encoded, nil 
	}

	raw := strings.TrimPrefix(encoded, encryptionPrefix)
	key := []byte(config.C.TokenEncryptionKey)
	if len(key) != 32 {
		return "", errors.New("TOKEN_ENCRYPTION_KEY must be exactly 32 bytes (256 bits)")
	}

	ciphertext, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
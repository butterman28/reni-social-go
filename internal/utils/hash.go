package utils

import (
	"crypto/sha256"
	"encoding/hex"
)

// CalculateHash generates a SHA-256 hash for a given byte slice and returns it as a hex string.
func CalculateHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

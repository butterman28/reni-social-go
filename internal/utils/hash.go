package utils

import (
	"crypto/sha256"
	"encoding/hex"
)

// CalculateHash generates a SHA-256 hash for a given byte slice and returns it as a hex string.
// CalculateHash godoc
// @Summary CalculateHash
// @Description Auto-generated thorough documentation for CalculateHash
// @Tags utils
// @Produce json
// @Param data body array true "data"
// @Success 200 {string} string "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func CalculateHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

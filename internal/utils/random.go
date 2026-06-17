package utils

import (
	"crypto/rand"
	"math/big"
)

const (
	numbers  = "0123456789"
	letters  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	alphanum = numbers + letters
)

// RandomNumbers generates a random numeric string of given length
func RandomNumbers(length int) string {
	return randomStringFromCharset(length, numbers)
}

// RandomAlphabets generates a random alphabetic string of given length
func RandomAlphabets(length int) string {
	return randomStringFromCharset(length, letters)
}

// RandomAlphaNumeric generates a random alphanumeric string of given length
func RandomAlphaNumeric(length int) string {
	return randomStringFromCharset(length, alphanum)
}

// Helper function to build random strings from a given charset
func randomStringFromCharset(length int, charset string) string {
	result := make([]byte, length)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			// fallback: return partial string if error occurs
			return string(result[:i])
		}
		result[i] = charset[n.Int64()]
	}
	return string(result)
}

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
// RandomNumbers godoc
// @Summary RandomNumbers
// @Description Auto-generated thorough documentation for RandomNumbers
// @Tags utils
// @Produce json
// @Param length body integer true "length"
// @Success 200 {string} string "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func RandomNumbers(length int) string {
	return randomStringFromCharset(length, numbers)
}

// RandomAlphabets generates a random alphabetic string of given length
// RandomAlphabets godoc
// @Summary RandomAlphabets
// @Description Auto-generated thorough documentation for RandomAlphabets
// @Tags utils
// @Produce json
// @Param length body integer true "length"
// @Success 200 {string} string "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func RandomAlphabets(length int) string {
	return randomStringFromCharset(length, letters)
}

// RandomAlphaNumeric generates a random alphanumeric string of given length
// RandomAlphaNumeric godoc
// @Summary RandomAlphaNumeric
// @Description Auto-generated thorough documentation for RandomAlphaNumeric
// @Tags utils
// @Produce json
// @Param length body integer true "length"
// @Success 200 {string} string "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func RandomAlphaNumeric(length int) string {
	return randomStringFromCharset(length, alphanum)
}

// Helper function to build random strings from a given charset
// randomStringFromCharset godoc
// @Summary randomStringFromCharset
// @Description Auto-generated thorough documentation for randomStringFromCharset
// @Tags utils
// @Produce json
// @Param length body integer true "length"
// @Param charset body string true "charset"
// @Success 200 {string} string "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
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

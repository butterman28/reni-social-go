package utils

import (
	"regexp"
	"strings"
)

// GenerateFriendlySlug generates a URL-friendly slug from a string, appending a random alphanumeric string to avoid collisions
// GenerateFriendlySlug godoc
// @Summary GenerateFriendlySlug
// @Description Auto-generated thorough documentation for GenerateFriendlySlug
// @Tags utils
// @Produce json
// @Param s body string true "s"
// @Success 200 {string} string "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func GenerateFriendlySlug(s string) (string, error) {
	// Lowercase the string
	s = strings.ToLower(s)

	// Replace non-alphanumeric characters with hyphens
	reg := regexp.MustCompile(`[^a-z0-9]+`)
	slug := reg.ReplaceAllString(s, "-")

	// Trim leading and trailing hyphens
	slug = strings.Trim(slug, "-")

	// Append a short random string to ensure uniqueness
	randomSuffix := strings.ToLower(RandomAlphaNumeric(6))
	
	if slug == "" {
		return "event-" + randomSuffix, nil
	}

	return slug + "-" + randomSuffix, nil
}

package utils

import (
	"regexp"
	"strings"
)

// GenerateFriendlySlug generates a URL-friendly slug from a string, appending a random alphanumeric string to avoid collisions
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

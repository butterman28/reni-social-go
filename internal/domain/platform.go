// NEW_FILE_CODE
package domain

import (
	"fmt"
	"strings"
)

// IsValid checks if the platform is one of the supported constants for publishing.
func (p Platform) IsValid() bool {
	switch p {
	case PlatformLinkedIn, PlatformMetaIG, PlatformMetaFB, PlatformX, PlatformTikTok:
		return true
	}
	return false
}

// SupportsArticles returns true if the platform supports article/link shares.
func (p Platform) SupportsArticles() bool {
	switch p {
	case PlatformLinkedIn, PlatformMetaFB, PlatformX:
		return true
	}
	return false
}

// RequiresMedia returns true if the platform requires at least one media URL.
func (p Platform) RequiresMedia() bool {
	switch p {
	case PlatformMetaIG, PlatformTikTok:
		return true
	}
	return false
}

// RequiresVideo returns true if the platform specifically requires a video file.
func (p Platform) RequiresVideo() bool {
	return p == PlatformTikTok
}

// IsVideoURL is a simple extension-based heuristic to check if a URL points to a video.
func IsVideoURL(mediaURL string) bool {
	lower := strings.ToLower(mediaURL)
	for _, ext := range []string{".mp4", ".mov", ".m4v", ".avi", ".webm"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// ValidateForPost checks if the platform is valid and compatible with the given post attributes.
func (p Platform) ValidateForPost(hasArticleURL bool, mediaURLs []string) error {
	if !p.IsValid() {
		return fmt.Errorf("unsupported platform: %s", p)
	}

	hasMedia := len(mediaURLs) > 0
	hasVideo := false
	if hasMedia {
		for _, u := range mediaURLs {
			if IsVideoURL(u) {
				hasVideo = true
				break
			}
		}
	}

	if hasArticleURL && !p.SupportsArticles() {
		return fmt.Errorf("%s does not support article/link shares", p)
	}
	if p.RequiresMedia() && !hasMedia {
		return fmt.Errorf("%s requires at least one media file", p)
	}
	if p.RequiresVideo() && !hasVideo {
		return fmt.Errorf("%s requires a video file (e.g., .mp4, .mov)", p)
	}
	return nil
}
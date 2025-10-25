package utils

import (
	"regexp"
	"strings"
)

// Slugify converts a string to a URL-friendly slug
// - Converts to lowercase
// - Replaces spaces with hyphens
// - Removes special characters
// - Removes consecutive hyphens
// - Trims leading/trailing hyphens
func Slugify(text string) string {
	// Convert to lowercase
	slug := strings.ToLower(text)

	// Replace spaces with hyphens
	slug = strings.ReplaceAll(slug, " ", "-")

	// Remove all non-alphanumeric characters except hyphens
	reg := regexp.MustCompile("[^a-z0-9-]+")
	slug = reg.ReplaceAllString(slug, "")

	// Replace multiple consecutive hyphens with single hyphen
	reg = regexp.MustCompile("-+")
	slug = reg.ReplaceAllString(slug, "-")

	// Trim hyphens from start and end
	slug = strings.Trim(slug, "-")

	return slug
}

// GenerateProductionDomain generates a production domain URL for an app
// Format: {slugified-app-name}-{last-6-chars-of-app-id}.rapidbuild.app
// Example: "My Cool App" with ID "2028362b-a14a-43ac-87d8-0e26c7401623"
//          becomes "my-cool-app-401623.rapidbuild.app"
func GenerateProductionDomain(appName, appID string) string {
	// Slugify the app name
	slug := Slugify(appName)

	// If slugification results in empty string, use "app" as default
	if slug == "" {
		slug = "app"
	}

	// Extract last 6 characters of app ID (removing hyphens first for cleaner result)
	cleanID := strings.ReplaceAll(appID, "-", "")
	idLength := len(cleanID)
	var suffix string
	if idLength >= 6 {
		suffix = cleanID[idLength-6:]
	} else {
		suffix = cleanID
	}

	// Combine: slug-suffix.rapidbuild.app
	domain := slug + "-" + suffix + ".rapidbuild.app"

	return domain
}

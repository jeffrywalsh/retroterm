package main

import (
	"regexp"
	"strings"
)

// GenerateSlug creates a URL-friendly slug from a BBS name
func GenerateSlug(name string) string {
	// Convert to lowercase
	slug := strings.ToLower(name)

	// Replace spaces and common separators with hyphens
	slug = regexp.MustCompile(`[\s_]+`).ReplaceAllString(slug, "-")

	// Remove any characters that aren't alphanumeric or hyphens
	slug = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(slug, "")

	// Remove leading/trailing hyphens
	slug = strings.Trim(slug, "-")

	// Collapse multiple hyphens into one
	slug = regexp.MustCompile(`-+`).ReplaceAllString(slug, "-")

	return slug
}

// FindBBSBySlug searches for a BBS entry by its slug
func FindBBSBySlug(slug string, bbsList []BBSEntry) *BBSEntry {
	for _, bbs := range bbsList {
		if GenerateSlug(bbs.Name) == slug {
			return &bbs
		}
	}
	return nil
}
package view

import "time"

// SiteName is the display name used in templates. Override per-project.
const SiteName = "Advanced Template"

// Tracking IDs and Turnstile site key, set once at startup from config.
var (
	PixelID          string
	GtagID           string
	TurnstileSiteKey string
)

// Year returns the current year for copyright notices.
func Year() int {
	return time.Now().Year()
}

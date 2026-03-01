package models

import (
	"time"
)

// Profile represents a surveillance tool (FRD Section 4.1.2).
type Profile struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`                          // P-01: Required
	Category           string    `json:"category"`                      // P-02: Required
	Manufacturer       string    `json:"manufacturer"`                  // P-03: Required
	DeploymentContext  string    `json:"deployment_context,omitempty"`  // P-04: Optional enum
	Observability      string    `json:"observability"`                 // P-05: Required enum
	Description        string    `json:"description,omitempty"`         // P-06: Optional
	UseCases           string    `json:"use_cases,omitempty"`           // P-07: Optional
	CommonLocations    string    `json:"common_locations,omitempty"`    // P-08: Optional
	KnownVulnerabilities string `json:"known_vulnerabilities,omitempty"` // P-09: Optional, comma-separated tags
	VisualIdentifiers  string    `json:"visual_identifiers,omitempty"`  // P-10: Optional
	Countermeasures    string    `json:"countermeasures,omitempty"`     // P-11: Optional
	References         string    `json:"references,omitempty"`          // P-12: Optional
	AvatarImageID      *int64    `json:"avatar_image_id,omitempty"`     // P-15: Optional FK to images
	Fingerprint        string    `json:"-"`                             // Hashed submitter fingerprint
	Deleted            bool      `json:"-"`                             // Soft delete flag
	CreatedAt          time.Time `json:"created_at"`                    // P-13: Auto
	UpdatedAt          time.Time `json:"updated_at"`                    // P-14: Auto
}

// Image represents a photograph or diagram attached to a profile (FRD Section 4.3).
type Image struct {
	ID        int64     `json:"id"`
	ProfileID int64     `json:"profile_id"`
	Filename  string    `json:"filename"`   // UUID-based filename on disk
	OrigName  string    `json:"orig_name"`  // Original uploaded filename
	Caption   string    `json:"caption"`    // FR-I-07: Optional, max 500 chars
	MimeType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// Pin represents a geographic sighting of a surveillance tool (FRD Section 4.4.2).
type Pin struct {
	ID            int64     `json:"id"`
	ProfileID     int64     `json:"profile_id"`      // M-01: Required FK
	Latitude      float64   `json:"latitude"`         // M-02: Required
	Longitude     float64   `json:"longitude"`        // M-02: Required
	LocationLabel string    `json:"location_label"`   // M-03: Optional
	Notes         string    `json:"notes"`            // M-04: Optional
	DateObserved  string    `json:"date_observed"`    // M-05: Optional (date string)
	Fingerprint   string    `json:"-"`                // Hashed submitter fingerprint
	Deleted       bool      `json:"-"`                // Soft delete flag
	SubmittedAt   time.Time `json:"submitted_at"`     // M-06: Auto
	UpdatedAt     time.Time `json:"updated_at"`

	// Joined fields (not stored, populated by queries)
	ProfileName     string `json:"profile_name,omitempty"`
	ProfileCategory string `json:"profile_category,omitempty"`
}

// ProfileFilter holds filter/search parameters for the profile browser.
type ProfileFilter struct {
	Search            string
	Category          string
	Manufacturer      string
	DeploymentContext string
	Observability     string
	Vulnerabilities   []string
	Page              int
	PageSize          int
}

// PinFilter holds filter parameters for the map view.
type PinFilter struct {
	ProfileID    int64
	Category     string
	Manufacturer string
}

// Pagination holds pagination metadata for list responses.
type Pagination struct {
	CurrentPage int
	TotalPages  int
	TotalItems  int
	PageSize    int
	HasPrev     bool
	HasNext     bool
}

// Valid category values for profiles.
var Categories = []string{
	"Physical",
	"Digital",
	"Audio",
	"Biometric",
	"Network",
	"Aerial",
	"Tracking",
	"Other",
}

// Valid deployment context values (FRD P-04).
var DeploymentContexts = []string{
	"Government / Law Enforcement",
	"Corporate",
	"Private Individual",
	"Multiple",
}

// Valid observability values (FRD P-05).
var ObservabilityLevels = []string{
	"Visible",
	"Covert",
	"Requires Equipment",
}

// Common vulnerability tags (FRD P-09).
var VulnerabilityTags = []string{
	"Susceptible to Jamming",
	"Unencrypted Transmission",
	"Publicly Documented Exploit",
	"Requires Line of Sight",
	"Signal Detectable",
	"Vulnerable to Spoofing",
	"Known Firmware Exploits",
	"Weak Authentication",
}

// IsValidCategory checks if a category value is in the allowed set.
func IsValidCategory(cat string) bool {
	for _, c := range Categories {
		if c == cat {
			return true
		}
	}
	return false
}

// IsValidObservability checks if an observability value is in the allowed set.
func IsValidObservability(obs string) bool {
	for _, o := range ObservabilityLevels {
		if o == obs {
			return true
		}
	}
	return false
}

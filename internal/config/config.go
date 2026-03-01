package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all operator-facing configuration values.
// Values are read from environment variables with sensible defaults.
type Config struct {
	// FederationEnabled gates all federation sync behavior.
	// When false, sync endpoints return 404 and no outbound peer requests are made.
	FederationEnabled bool

	// MapTileSource controls the tile URL Leaflet.js uses.
	// Valid values: "osm" (default), "protomaps".
	MapTileSource string

	// ProtomapsEndpoint is the base URL of the local Protomaps tile server.
	// Only used when MapTileSource = "protomaps".
	ProtomapsEndpoint string

	// ImageMaxSizeMB is the maximum allowed upload size per image in megabytes.
	ImageMaxSizeMB int

	// DataPath is the root path for image storage on the local filesystem.
	DataPath string

	// DBPath is the path to the SQLite database file.
	DBPath string

	// PageSize is the default number of profiles per page in the browser.
	PageSize int

	// ListenAddr is the address the HTTP server binds to.
	ListenAddr string

	// BaseURL is the externally-accessible URL of the application (for links in exports).
	BaseURL string
}

// Load reads configuration from environment variables with defaults.
func Load() *Config {
	return &Config{
		FederationEnabled: envBool("FEDERATION_ENABLED", false),
		MapTileSource:     envString("MAP_TILE_SOURCE", "osm"),
		ProtomapsEndpoint: envString("PROTOMAPS_ENDPOINT", ""),
		ImageMaxSizeMB:    envInt("IMAGE_MAX_SIZE_MB", 20),
		DataPath:          envString("DATA_PATH", "/data/images"),
		DBPath:            envString("DB_PATH", "/data/db/argus.db"),
		PageSize:          envInt("PAGE_SIZE", 50),
		ListenAddr:        envString("LISTEN_ADDR", ":8080"),
		BaseURL:           envString("BASE_URL", "http://localhost:8080"),
	}
}

// TileURL returns the tile URL template for Leaflet.js based on the configured source.
func (c *Config) TileURL() string {
	switch strings.ToLower(c.MapTileSource) {
	case "protomaps":
		if c.ProtomapsEndpoint != "" {
			return c.ProtomapsEndpoint + "/{z}/{x}/{y}.png"
		}
		return ""
	default:
		return "https://tile.openstreetmap.org/{z}/{x}/{y}.png"
	}
}

// TileAttribution returns the attribution string for the configured tile source.
func (c *Config) TileAttribution() string {
	switch strings.ToLower(c.MapTileSource) {
	case "protomaps":
		return `&copy; <a href="https://protomaps.com">Protomaps</a> &copy; <a href="https://openstreetmap.org">OpenStreetMap</a>`
	default:
		return `&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors`
	}
}

// ImageMaxBytes returns the maximum image size in bytes.
func (c *Config) ImageMaxBytes() int64 {
	return int64(c.ImageMaxSizeMB) * 1024 * 1024
}

// Validate checks that the configuration is internally consistent.
func (c *Config) Validate() error {
	if c.MapTileSource == "protomaps" && c.ProtomapsEndpoint == "" {
		return fmt.Errorf("PROTOMAPS_ENDPOINT must be set when MAP_TILE_SOURCE=protomaps")
	}
	if c.ImageMaxSizeMB < 1 || c.ImageMaxSizeMB > 100 {
		return fmt.Errorf("IMAGE_MAX_SIZE_MB must be between 1 and 100")
	}
	if c.PageSize < 10 || c.PageSize > 500 {
		return fmt.Errorf("PAGE_SIZE must be between 10 and 500")
	}
	return nil
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		return strings.ToLower(v) == "true" || v == "1"
	}
	return fallback
}

package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/argus-platform/argus/internal/config"
	"github.com/argus-platform/argus/internal/database"
	"github.com/argus-platform/argus/internal/handlers"
	"github.com/argus-platform/argus/internal/middleware"
	"github.com/argus-platform/argus/internal/models"
	"github.com/argus-platform/argus/internal/storage"
)

func main() {
	// Load configuration
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Ensure data directories exist
	if err := os.MkdirAll(cfg.DataPath, 0755); err != nil {
		log.Fatalf("Cannot create data directory %s: %v", cfg.DataPath, err)
	}
	dbDir := filepath.Dir(cfg.DBPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		log.Fatalf("Cannot create database directory %s: %v", dbDir, err)
	}

	// Open database
	db, err := database.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("Cannot open database: %v", err)
	}
	defer db.Close()

	// Initialize image store
	store := storage.NewLocalStore(cfg.DataPath)

	// Parse templates
	tmpl := loadTemplates()

	// Create application handler
	app := &handlers.App{
		DB:     db,
		Store:  store,
		Config: cfg,
		Tmpl:   tmpl,
	}

	// Set up routes
	mux := http.NewServeMux()

	// Static files
	mux.Handle("/static/", http.StripPrefix("/static/",
		http.FileServer(http.Dir("static"))))

	// Profile routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/profiles" {
			app.ProfileList(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/profiles", app.ProfileList)
	mux.HandleFunc("/profiles/new", app.ProfileCreate)
	mux.HandleFunc("/profiles/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/edit"):
			app.ProfileEdit(w, r)
		case strings.HasSuffix(path, "/delete"):
			app.ProfileDelete(w, r)
		case strings.HasSuffix(path, "/images"):
			app.ImageUpload(w, r)
		default:
			app.ProfileDetail(w, r)
		}
	})

	// Image routes
	mux.HandleFunc("/images/file/", app.ImageServe)
	mux.HandleFunc("/images/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/delete") {
			app.ImageDelete(w, r)
			return
		}
		http.NotFound(w, r)
	})

	// Map routes
	mux.HandleFunc("/map", app.MapView)

	// API routes
	mux.HandleFunc("/api/pins", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			app.PinsJSON(w, r)
		case http.MethodPost:
			app.PinCreate(w, r)
		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/pins/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/delete") {
			app.PinDelete(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/profiles/search", app.ProfileSearch)

	// Federation sync endpoints (gated by FEDERATION_ENABLED)
	mux.HandleFunc("/api/v1/profiles", app.FederationGuard(app.SyncProfiles))
	mux.HandleFunc("/api/v1/pins", app.FederationGuard(app.SyncPins))
	mux.HandleFunc("/api/v1/since", app.FederationGuard(app.SyncSince))

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		profiles, pins, images, err := db.Stats()
		if err != nil {
			http.Error(w, "unhealthy", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "ok",
			"profiles": profiles,
			"pins":     pins,
			"images":   images,
		})
	})

	// Apply middleware
	handler := middleware.SecurityHeaders(middleware.RequestLogger(mux))

	// Start server
	log.Printf("Argus starting on %s", cfg.ListenAddr)
	log.Printf("  Database: %s", cfg.DBPath)
	log.Printf("  Images:   %s", cfg.DataPath)
	log.Printf("  Tiles:    %s", cfg.MapTileSource)
	log.Printf("  Federation: %v", cfg.FederationEnabled)
	log.Printf("  Page size: %d", cfg.PageSize)

	if err := http.ListenAndServe(cfg.ListenAddr, handler); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// loadTemplates parses all HTML templates with custom functions.
func loadTemplates() *template.Template {
	funcMap := template.FuncMap{
		"lower": strings.ToLower,
		"truncate": func(max int, s string) string {
			if len(s) <= max {
				return s
			}
			return s[:max] + "…"
		},
		"contains": strings.Contains,
		"splitComma": func(s string) []string {
			if s == "" {
				return nil
			}
			parts := strings.Split(s, ",")
			var result []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					result = append(result, p)
				}
			}
			return result
		},
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"pinsJSON": func(pins []models.Pin) template.JS {
			type miniPin struct {
				Lat   float64 `json:"lat"`
				Lng   float64 `json:"lng"`
				Label string  `json:"label"`
			}
			var mp []miniPin
			for _, p := range pins {
				mp = append(mp, miniPin{Lat: p.Latitude, Lng: p.Longitude, Label: p.LocationLabel})
			}
			b, _ := json.Marshal(mp)
			return template.JS(b)
		},
		"printf": fmt.Sprintf,
	}

	tmpl := template.New("").Funcs(funcMap)

	// Parse all template files
	patterns := []string{
		"templates/layouts/*.html",
		"templates/profiles/*.html",
		"templates/map/*.html",
		"templates/partials/*.html",
	}

	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil {
			log.Fatalf("Error globbing templates %s: %v", pattern, err)
		}
		for _, f := range files {
			if _, err := tmpl.ParseFiles(f); err != nil {
				log.Fatalf("Error parsing template %s: %v", f, err)
			}
		}
	}

	return tmpl
}

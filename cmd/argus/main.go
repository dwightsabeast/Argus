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

	"github.com/dwightsabeast/argus/internal/config"
	"github.com/dwightsabeast/argus/internal/database"
	"github.com/dwightsabeast/argus/internal/handlers"
	"github.com/dwightsabeast/argus/internal/middleware"
	"github.com/dwightsabeast/argus/internal/models"
	"github.com/dwightsabeast/argus/internal/storage"
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
func loadTemplates() map[string]*template.Template {
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

	templates := make(map[string]*template.Template)
	baseLayout := "templates/layouts/base.html"

	// Page templates: each is parsed independently with the base layout
	// so their {{define "content"}}, {{define "title"}}, etc. don't collide.
	pages := map[string][]string{
		"profile_list":   {baseLayout, "templates/profiles/list.html", "templates/profiles/list_partial.html"},
		"profile_detail": {baseLayout, "templates/profiles/detail.html"},
		"profile_form":   {baseLayout, "templates/profiles/form.html"},
		"map_view":       {baseLayout, "templates/map/index.html"},
	}

	for name, files := range pages {
		t := template.New("").Funcs(funcMap)
		t, err := t.ParseFiles(files...)
		if err != nil {
			log.Fatalf("Error parsing template %s: %v", name, err)
		}
		templates[name] = t
	}

	// Fragment templates: no layout, parsed standalone.
	fragments := map[string]string{
		"profile_list_partial":   "templates/profiles/list_partial.html",
		"profile_search_results": "templates/profiles/search_results.html",
	}

	for name, fragFile := range fragments {
		t := template.New("").Funcs(funcMap)
		t, err := t.ParseFiles(fragFile)
		if err != nil {
			log.Fatalf("Error parsing fragment template %s (%s): %v", name, fragFile, err)
		}
		templates[name] = t
	}

	return templates
}

package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

// FederationGuard returns 404 when federation is disabled (FR-F-01).
func (app *App) FederationGuard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !app.Config.FederationEnabled {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

// SyncProfiles handles GET /api/v1/profiles (FR-F-02).
func (app *App) SyncProfiles(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 500 {
		pageSize = 100
	}

	// Default to epoch for full dump
	since := time.Unix(0, 0)

	profiles, err := app.DB.ProfilesSince(since, page, pageSize)
	if err != nil {
		log.Printf("ERROR syncing profiles: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profiles)
}

// SyncPins handles GET /api/v1/pins (FR-F-02).
func (app *App) SyncPins(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 500 {
		pageSize = 100
	}

	since := time.Unix(0, 0)

	pins, err := app.DB.PinsSince(since, page, pageSize)
	if err != nil {
		log.Printf("ERROR syncing pins: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pins)
}

// SyncSince handles GET /api/v1/since?timestamp=X (FR-F-02, FR-F-03).
// Returns profiles and pins modified after the given Unix timestamp.
func (app *App) SyncSince(w http.ResponseWriter, r *http.Request) {
	tsStr := r.URL.Query().Get("timestamp")
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil || ts < 0 {
		http.Error(w, "Valid Unix timestamp required", http.StatusBadRequest)
		return
	}

	since := time.Unix(ts, 0)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 500 {
		pageSize = 100
	}

	profiles, err := app.DB.ProfilesSince(since, page, pageSize)
	if err != nil {
		log.Printf("ERROR syncing profiles since %d: %v", ts, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	pins, err := app.DB.PinsSince(since, page, pageSize)
	if err != nil {
		log.Printf("ERROR syncing pins since %d: %v", ts, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"since":    ts,
		"profiles": profiles,
		"pins":     pins,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

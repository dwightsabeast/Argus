package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/dwightsabeast/argus/internal/middleware"
	"github.com/dwightsabeast/argus/internal/models"
)

// MapView handles GET /map (FR-M-01).
func (app *App) MapView(w http.ResponseWriter, r *http.Request) {
	profiles, _ := app.DB.AllProfilesForDropdown()
	manufacturers, _ := app.DB.ListManufacturers()

	data := map[string]interface{}{
		"Profiles":      profiles,
		"Categories":    models.Categories,
		"Manufacturers": manufacturers,
		"Config":        app.Config,
		"TileURL":       app.Config.TileURL(),
		"TileAttrib":    app.Config.TileAttribution(),
		"NewPin":        r.URL.Query().Get("new_pin") == "true",
		"PreselectedID": r.URL.Query().Get("profile_id"),
	}

	app.render(w, "map_view", data)
}

// PinsJSON handles GET /api/pins (returns all pins as GeoJSON for the map).
func (app *App) PinsJSON(w http.ResponseWriter, r *http.Request) {
	filter := models.PinFilter{
		Category:     r.URL.Query().Get("category"),
		Manufacturer: r.URL.Query().Get("manufacturer"),
	}
	if pidStr := r.URL.Query().Get("profile_id"); pidStr != "" {
		filter.ProfileID, _ = strconv.ParseInt(pidStr, 10, 64)
	}

	pins, err := app.DB.ListPins(filter)
	if err != nil {
		log.Printf("ERROR listing pins: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Convert to GeoJSON FeatureCollection for Leaflet
	type Properties struct {
		ID            int64  `json:"id"`
		ProfileID     int64  `json:"profile_id"`
		ProfileName   string `json:"profile_name"`
		Category      string `json:"category"`
		LocationLabel string `json:"location_label"`
		Notes         string `json:"notes"`
		DateObserved  string `json:"date_observed"`
	}

	type Geometry struct {
		Type        string    `json:"type"`
		Coordinates []float64 `json:"coordinates"` // [lon, lat] per GeoJSON spec
	}

	type Feature struct {
		Type       string     `json:"type"`
		Geometry   Geometry   `json:"geometry"`
		Properties Properties `json:"properties"`
	}

	type FeatureCollection struct {
		Type     string    `json:"type"`
		Features []Feature `json:"features"`
	}

	fc := FeatureCollection{
		Type:     "FeatureCollection",
		Features: make([]Feature, 0, len(pins)),
	}

	for _, pin := range pins {
		fc.Features = append(fc.Features, Feature{
			Type: "Feature",
			Geometry: Geometry{
				Type:        "Point",
				Coordinates: []float64{pin.Longitude, pin.Latitude},
			},
			Properties: Properties{
				ID:            pin.ID,
				ProfileID:     pin.ProfileID,
				ProfileName:   pin.ProfileName,
				Category:      pin.ProfileCategory,
				LocationLabel: pin.LocationLabel,
				Notes:         pin.Notes,
				DateObserved:  pin.DateObserved,
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fc)
}

// PinCreate handles POST /api/pins (FR-M-05 through FR-M-08).
func (app *App) PinCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	profileID, err := strconv.ParseInt(r.FormValue("profile_id"), 10, 64)
	if err != nil || profileID == 0 {
		http.Error(w, "A linked profile is required (FR-M-08)", http.StatusBadRequest)
		return
	}

	// Verify profile exists
	profile, err := app.DB.GetProfile(profileID)
	if err != nil || profile == nil {
		http.Error(w, "Selected profile not found", http.StatusBadRequest)
		return
	}

	lat, err := strconv.ParseFloat(r.FormValue("latitude"), 64)
	if err != nil || lat < -90 || lat > 90 {
		http.Error(w, "Valid latitude (-90 to 90) is required", http.StatusBadRequest)
		return
	}

	lng, err := strconv.ParseFloat(r.FormValue("longitude"), 64)
	if err != nil || lng < -180 || lng > 180 {
		http.Error(w, "Valid longitude (-180 to 180) is required", http.StatusBadRequest)
		return
	}

	pin := &models.Pin{
		ProfileID:     profileID,
		Latitude:      lat,
		Longitude:     lng,
		LocationLabel: middleware.SanitizeStringRaw(r.FormValue("location_label")),
		Notes:         middleware.SanitizeStringRaw(r.FormValue("notes")),
		DateObserved:  middleware.SanitizeStringRaw(r.FormValue("date_observed")),
		Fingerprint:   middleware.Fingerprint(r),
	}

	id, err := app.DB.CreatePin(pin)
	if err != nil {
		log.Printf("ERROR creating pin: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		// Return success response that the JS can handle
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id": %d, "success": true}`, id)
		return
	}

	http.Redirect(w, r, "/map", http.StatusSeeOther)
}

// PinDelete handles POST /api/pins/{id}/delete (FR-M-11).
func (app *App) PinDelete(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/delete")
	id := extractID(path, "/api/pins/")
	if id == 0 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if err := app.DB.DeletePin(id); err != nil {
		log.Printf("ERROR deleting pin %d: %v", id, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success": true}`)
		return
	}

	http.Redirect(w, r, "/map", http.StatusSeeOther)
}

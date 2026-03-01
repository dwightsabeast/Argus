package handlers

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/dwightsabeast/argus/internal/config"
	"github.com/dwightsabeast/argus/internal/database"
	"github.com/dwightsabeast/argus/internal/middleware"
	"github.com/dwightsabeast/argus/internal/models"
	"github.com/dwightsabeast/argus/internal/storage"
)

// App holds shared dependencies for all handlers.
type App struct {
	DB     *database.DB
	Store  storage.ImageStore
	Config *config.Config
	Tmpl   map[string]*template.Template
}

// ProfileList handles GET / and GET /profiles (FR-B-01, FR-P-02).
func (app *App) ProfileList(w http.ResponseWriter, r *http.Request) {
	filter := models.ProfileFilter{
		Search:            r.URL.Query().Get("search"),
		Category:          r.URL.Query().Get("category"),
		Manufacturer:      r.URL.Query().Get("manufacturer"),
		DeploymentContext: r.URL.Query().Get("deployment_context"),
		Observability:     r.URL.Query().Get("observability"),
		PageSize:          app.Config.PageSize,
	}

	// Handle vulnerability filter (multi-select)
	if vulns := r.URL.Query().Get("vulnerabilities"); vulns != "" {
		filter.Vulnerabilities = strings.Split(vulns, ",")
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	filter.Page = page

	profiles, pagination, err := app.DB.ListProfiles(filter)
	if err != nil {
		log.Printf("ERROR listing profiles: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	manufacturers, _ := app.DB.ListManufacturers()

	// Load thumbnail for each profile
	type ProfileWithThumb struct {
		models.Profile
		ThumbnailURL string
	}
	var profilesWithThumbs []ProfileWithThumb
	for _, p := range profiles {
		pwt := ProfileWithThumb{Profile: p}
		if img, err := app.DB.GetFirstImageForProfile(p.ID); err == nil && img != nil {
			pwt.ThumbnailURL = fmt.Sprintf("/images/file/%d/%s", img.ProfileID, img.Filename)
		}
		profilesWithThumbs = append(profilesWithThumbs, pwt)
	}

	data := map[string]interface{}{
		"Profiles":           profilesWithThumbs,
		"Pagination":         pagination,
		"Filter":             filter,
		"Categories":         models.Categories,
		"Manufacturers":      manufacturers,
		"DeploymentContexts": models.DeploymentContexts,
		"ObservabilityLevels": models.ObservabilityLevels,
		"VulnerabilityTags":  models.VulnerabilityTags,
		"Config":             app.Config,
	}

	// If HTMX request, return only the profile list partial
	if r.Header.Get("HX-Request") == "true" {
		app.render(w, "profile_list_partial", data)
		return
	}

	app.render(w, "profile_list", data)
}

// ProfileDetail handles GET /profiles/{id} (FR-P-03, FR-B-07).
func (app *App) ProfileDetail(w http.ResponseWriter, r *http.Request) {
	id := extractID(r.URL.Path, "/profiles/")
	if id == 0 {
		http.NotFound(w, r)
		return
	}

	profile, err := app.DB.GetProfile(id)
	if err != nil {
		log.Printf("ERROR getting profile %d: %v", id, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if profile == nil {
		http.NotFound(w, r)
		return
	}

	images, err := app.DB.ListImagesByProfile(id)
	if err != nil {
		log.Printf("ERROR listing images for profile %d: %v", id, err)
	}

	pins, err := app.DB.ListPinsByProfile(id)
	if err != nil {
		log.Printf("ERROR listing pins for profile %d: %v", id, err)
	}

	// Find avatar image for hero display
	var avatarImage *models.Image
	if profile.AvatarImageID != nil {
		for i := range images {
			if images[i].ID == *profile.AvatarImageID {
				avatarImage = &images[i]
				break
			}
		}
	}

	data := map[string]interface{}{
		"Profile":     profile,
		"Images":      images,
		"AvatarImage": avatarImage,
		"Pins":        pins,
		"Config":      app.Config,
	}

	app.render(w, "profile_detail", data)
}

// ProfileCreate handles GET/POST /profiles/new (FR-P-01).
func (app *App) ProfileCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		data := map[string]interface{}{
			"Profile":             &models.Profile{},
			"Categories":         models.Categories,
			"DeploymentContexts": models.DeploymentContexts,
			"ObservabilityLevels": models.ObservabilityLevels,
			"VulnerabilityTags":  models.VulnerabilityTags,
			"IsEdit":             false,
			"Config":             app.Config,
		}
		app.render(w, "profile_form", data)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	profile := profileFromForm(r)
	profile.Fingerprint = middleware.Fingerprint(r)

	// Validate required fields
	if profile.Name == "" || profile.Category == "" || profile.Manufacturer == "" || profile.Observability == "" {
		data := map[string]interface{}{
			"Profile":             profile,
			"Categories":         models.Categories,
			"DeploymentContexts": models.DeploymentContexts,
			"ObservabilityLevels": models.ObservabilityLevels,
			"VulnerabilityTags":  models.VulnerabilityTags,
			"IsEdit":             false,
			"Error":              "Name, Category, Manufacturer, and Observability are required.",
			"Config":             app.Config,
		}
		w.WriteHeader(http.StatusBadRequest)
		app.render(w, "profile_form", data)
		return
	}

	id, err := app.DB.CreateProfile(profile)
	if err != nil {
		log.Printf("ERROR creating profile: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// If this was triggered from the pin form, return to pin form with profile pre-selected
	if r.URL.Query().Get("return_to") == "pin" {
		w.Header().Set("HX-Redirect", fmt.Sprintf("/map?new_pin=true&profile_id=%d", id))
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/profiles/%d", id), http.StatusSeeOther)
}

// ProfileEdit handles GET/POST /profiles/{id}/edit (FR-P-04).
func (app *App) ProfileEdit(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path like /profiles/123/edit
	path := strings.TrimSuffix(r.URL.Path, "/edit")
	id := extractID(path, "/profiles/")
	if id == 0 {
		http.NotFound(w, r)
		return
	}

	profile, err := app.DB.GetProfile(id)
	if err != nil || profile == nil {
		http.NotFound(w, r)
		return
	}

	if r.Method == http.MethodGet {
		data := map[string]interface{}{
			"Profile":             profile,
			"Categories":         models.Categories,
			"DeploymentContexts": models.DeploymentContexts,
			"ObservabilityLevels": models.ObservabilityLevels,
			"VulnerabilityTags":  models.VulnerabilityTags,
			"IsEdit":             true,
			"Config":             app.Config,
		}
		app.render(w, "profile_form", data)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	updated := profileFromForm(r)
	updated.ID = id

	if updated.Name == "" || updated.Category == "" || updated.Manufacturer == "" || updated.Observability == "" {
		data := map[string]interface{}{
			"Profile":             updated,
			"Categories":         models.Categories,
			"DeploymentContexts": models.DeploymentContexts,
			"ObservabilityLevels": models.ObservabilityLevels,
			"VulnerabilityTags":  models.VulnerabilityTags,
			"IsEdit":             true,
			"Error":              "Name, Category, Manufacturer, and Observability are required.",
			"Config":             app.Config,
		}
		w.WriteHeader(http.StatusBadRequest)
		app.render(w, "profile_form", data)
		return
	}

	if err := app.DB.UpdateProfile(updated); err != nil {
		log.Printf("ERROR updating profile %d: %v", id, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/profiles/%d", id), http.StatusSeeOther)
}

// ProfileDelete handles POST /profiles/{id}/delete (FR-P-05).
func (app *App) ProfileDelete(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/delete")
	id := extractID(path, "/profiles/")
	if id == 0 {
		http.NotFound(w, r)
		return
	}

	if err := app.DB.DeleteProfile(id); err != nil {
		log.Printf("ERROR deleting profile %d: %v", id, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/profiles")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/profiles", http.StatusSeeOther)
}

// ProfileSearch handles GET /api/profiles/search (for pin form dropdown).
func (app *App) ProfileSearch(w http.ResponseWriter, r *http.Request) {
	term := r.URL.Query().Get("q")
	profiles, err := app.DB.SearchProfiles(term, 20)
	if err != nil {
		log.Printf("ERROR searching profiles: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Profiles": profiles,
	}
	app.render(w, "profile_search_results", data)
}

// profileFromForm extracts a Profile from form values.
func profileFromForm(r *http.Request) *models.Profile {
	vulns := r.Form["known_vulnerabilities"]
	return &models.Profile{
		Name:                 middleware.SanitizeStringRaw(r.FormValue("name")),
		Category:             r.FormValue("category"),
		Manufacturer:         middleware.SanitizeStringRaw(r.FormValue("manufacturer")),
		DeploymentContext:    r.FormValue("deployment_context"),
		Observability:        r.FormValue("observability"),
		Description:          middleware.SanitizeStringRaw(r.FormValue("description")),
		UseCases:             middleware.SanitizeStringRaw(r.FormValue("use_cases")),
		CommonLocations:      middleware.SanitizeStringRaw(r.FormValue("common_locations")),
		KnownVulnerabilities: strings.Join(vulns, ","),
		VisualIdentifiers:    middleware.SanitizeStringRaw(r.FormValue("visual_identifiers")),
		Countermeasures:      middleware.SanitizeStringRaw(r.FormValue("countermeasures")),
		References:           middleware.SanitizeStringRaw(r.FormValue("references")),
	}
}

func (app *App) render(w http.ResponseWriter, name string, data interface{}) {
	t, ok := app.Tmpl[name]
	if !ok {
		log.Printf("ERROR: template %q not found", name)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	// Page templates include the base layout; fragments don't.
	execName := "layout"
	if t.Lookup("layout") == nil {
		execName = name
	}
	if err := t.ExecuteTemplate(w, execName, data); err != nil {
		log.Printf("ERROR rendering template %s: %v", name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func extractID(path, prefix string) int64 {
	s := strings.TrimPrefix(path, prefix)
	s = strings.Split(s, "/")[0]
	id, _ := strconv.ParseInt(s, 10, 64)
	return id
}

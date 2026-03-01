package handlers

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/argus-platform/argus/internal/middleware"
	"github.com/argus-platform/argus/internal/models"
	"github.com/argus-platform/argus/internal/storage"

	"crypto/rand"
	"encoding/hex"
)

// ImageUpload handles POST /profiles/{id}/images (FR-I-01).
func (app *App) ImageUpload(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/images")
	profileID := extractID(path, "/profiles/")
	if profileID == 0 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Verify profile exists
	profile, err := app.DB.GetProfile(profileID)
	if err != nil || profile == nil {
		http.NotFound(w, r)
		return
	}

	// Enforce max upload size (FR-I-03)
	r.Body = http.MaxBytesReader(w, r.Body, app.Config.ImageMaxBytes()+1024)

	if err := r.ParseMultipartForm(app.Config.ImageMaxBytes()); err != nil {
		http.Error(w, fmt.Sprintf("File too large. Maximum size: %d MB", app.Config.ImageMaxSizeMB), http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["images"]
	if len(files) == 0 {
		http.Error(w, "No files uploaded", http.StatusBadRequest)
		return
	}

	for _, fileHeader := range files {
		// Validate MIME type (FR-I-02)
		mimeType := fileHeader.Header.Get("Content-Type")
		if !storage.ValidateMimeType(mimeType) {
			http.Error(w, fmt.Sprintf("Unsupported format: %s. Accepted: JPEG, PNG, WebP", mimeType), http.StatusBadRequest)
			return
		}

		// Enforce per-file size limit
		if fileHeader.Size > app.Config.ImageMaxBytes() {
			http.Error(w, fmt.Sprintf("File %s exceeds maximum size of %d MB", fileHeader.Filename, app.Config.ImageMaxSizeMB), http.StatusBadRequest)
			return
		}

		file, err := fileHeader.Open()
		if err != nil {
			log.Printf("ERROR opening uploaded file: %v", err)
			http.Error(w, "Failed to process upload", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// Generate UUID-based filename
		uuid := generateUUID()
		ext := storage.ExtensionFromMime(mimeType)
		filename := uuid + ext

		// Save to disk with EXIF stripping (FR-I-08)
		if err := app.Store.Save(profileID, filename, file, mimeType); err != nil {
			log.Printf("ERROR saving image: %v", err)
			http.Error(w, "Failed to save image", http.StatusInternalServerError)
			return
		}

		// Get caption from form
		caption := middleware.SanitizeStringRaw(r.FormValue("caption"))
		if len(caption) > 500 {
			caption = caption[:500]
		}

		// Create database record
		img := &models.Image{
			ProfileID: profileID,
			Filename:  filename,
			OrigName:  filepath.Base(fileHeader.Filename),
			Caption:   caption,
			MimeType:  mimeType,
			SizeBytes: fileHeader.Size,
		}
		if _, err := app.DB.CreateImage(img); err != nil {
			log.Printf("ERROR creating image record: %v", err)
			// Clean up the file
			app.Store.Delete(profileID, filename)
			http.Error(w, "Failed to save image record", http.StatusInternalServerError)
			return
		}
	}

	// Redirect back to profile detail
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", fmt.Sprintf("/profiles/%d", profileID))
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/profiles/%d", profileID), http.StatusSeeOther)
}

// ImageServe handles GET /images/file/{profileID}/{filename} (FR-I-04).
func (app *App) ImageServe(w http.ResponseWriter, r *http.Request) {
	// Parse /images/file/{profileID}/{filename}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/images/file/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}

	profileID := int64(0)
	fmt.Sscanf(parts[0], "%d", &profileID)
	filename := filepath.Base(parts[1]) // Prevent path traversal

	if profileID == 0 || filename == "" || filename == "." {
		http.NotFound(w, r)
		return
	}

	path := app.Store.Path(profileID, filename)
	http.ServeFile(w, r, path)
}

// ImageDelete handles POST /images/{id}/delete (FR-I-06).
func (app *App) ImageDelete(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/delete")
	id := extractID(path, "/images/")
	if id == 0 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	filename, profileID, err := app.DB.DeleteImage(id)
	if err != nil {
		log.Printf("ERROR deleting image %d: %v", id, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Remove from disk
	if err := app.Store.Delete(profileID, filename); err != nil {
		log.Printf("WARNING: failed to delete image file %s: %v", filename, err)
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", fmt.Sprintf("/profiles/%d", profileID))
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/profiles/%d", profileID), http.StatusSeeOther)
}

func generateUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: use timestamp-based
		return fmt.Sprintf("%d", b)
	}
	return hex.EncodeToString(b)
}

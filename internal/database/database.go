package database

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/argus-platform/argus/internal/models"

	_ "modernc.org/sqlite"
)

// DB wraps the sql.DB connection and provides all data access methods.
type DB struct {
	conn *sql.DB
}

// Open creates or opens the SQLite database at the given path and runs migrations.
func Open(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	conn.SetMaxOpenConns(1) // SQLite handles one writer at a time
	conn.SetMaxIdleConns(1)

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS profiles (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		name            TEXT NOT NULL,
		category        TEXT NOT NULL,
		manufacturer    TEXT NOT NULL DEFAULT '',
		deployment_ctx  TEXT NOT NULL DEFAULT '',
		observability   TEXT NOT NULL DEFAULT 'Visible',
		description     TEXT NOT NULL DEFAULT '',
		use_cases       TEXT NOT NULL DEFAULT '',
		common_locations TEXT NOT NULL DEFAULT '',
		known_vulns     TEXT NOT NULL DEFAULT '',
		visual_ids      TEXT NOT NULL DEFAULT '',
		countermeasures TEXT NOT NULL DEFAULT '',
		refs            TEXT NOT NULL DEFAULT '',
		fingerprint     TEXT NOT NULL DEFAULT '',
		deleted         INTEGER NOT NULL DEFAULT 0,
		created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
		updated_at      DATETIME NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS images (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		profile_id  INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
		filename    TEXT NOT NULL,
		orig_name   TEXT NOT NULL DEFAULT '',
		caption     TEXT NOT NULL DEFAULT '',
		mime_type   TEXT NOT NULL DEFAULT '',
		size_bytes  INTEGER NOT NULL DEFAULT 0,
		created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS pins (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		profile_id      INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
		latitude        REAL NOT NULL,
		longitude       REAL NOT NULL,
		location_label  TEXT NOT NULL DEFAULT '',
		notes           TEXT NOT NULL DEFAULT '',
		date_observed   TEXT NOT NULL DEFAULT '',
		fingerprint     TEXT NOT NULL DEFAULT '',
		deleted         INTEGER NOT NULL DEFAULT 0,
		submitted_at    DATETIME NOT NULL DEFAULT (datetime('now')),
		updated_at      DATETIME NOT NULL DEFAULT (datetime('now'))
	);

	CREATE INDEX IF NOT EXISTS idx_profiles_category ON profiles(category) WHERE deleted = 0;
	CREATE INDEX IF NOT EXISTS idx_profiles_manufacturer ON profiles(manufacturer) WHERE deleted = 0;
	CREATE INDEX IF NOT EXISTS idx_profiles_deleted ON profiles(deleted);
	CREATE INDEX IF NOT EXISTS idx_profiles_updated ON profiles(updated_at);
	CREATE INDEX IF NOT EXISTS idx_images_profile ON images(profile_id);
	CREATE INDEX IF NOT EXISTS idx_pins_profile ON pins(profile_id) WHERE deleted = 0;
	CREATE INDEX IF NOT EXISTS idx_pins_deleted ON pins(deleted);
	CREATE INDEX IF NOT EXISTS idx_pins_updated ON pins(updated_at);
	`
	_, err := db.conn.Exec(schema)
	return err
}

// --- Profile Operations ---

// CreateProfile inserts a new profile and returns its ID.
func (db *DB) CreateProfile(p *models.Profile) (int64, error) {
	now := time.Now().UTC()
	res, err := db.conn.Exec(`
		INSERT INTO profiles (name, category, manufacturer, deployment_ctx, observability,
			description, use_cases, common_locations, known_vulns, visual_ids,
			countermeasures, refs, fingerprint, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Category, p.Manufacturer, p.DeploymentContext, p.Observability,
		p.Description, p.UseCases, p.CommonLocations, p.KnownVulnerabilities,
		p.VisualIdentifiers, p.Countermeasures, p.References, p.Fingerprint,
		now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateProfile updates an existing profile by ID.
func (db *DB) UpdateProfile(p *models.Profile) error {
	_, err := db.conn.Exec(`
		UPDATE profiles SET
			name = ?, category = ?, manufacturer = ?, deployment_ctx = ?,
			observability = ?, description = ?, use_cases = ?, common_locations = ?,
			known_vulns = ?, visual_ids = ?, countermeasures = ?, refs = ?,
			updated_at = datetime('now')
		WHERE id = ? AND deleted = 0`,
		p.Name, p.Category, p.Manufacturer, p.DeploymentContext,
		p.Observability, p.Description, p.UseCases, p.CommonLocations,
		p.KnownVulnerabilities, p.VisualIdentifiers, p.Countermeasures,
		p.References, p.ID,
	)
	return err
}

// DeleteProfile performs a soft delete on a profile.
func (db *DB) DeleteProfile(id int64) error {
	_, err := db.conn.Exec(`UPDATE profiles SET deleted = 1, updated_at = datetime('now') WHERE id = ?`, id)
	return err
}

// GetProfile retrieves a single profile by ID.
func (db *DB) GetProfile(id int64) (*models.Profile, error) {
	p := &models.Profile{}
	err := db.conn.QueryRow(`
		SELECT id, name, category, manufacturer, deployment_ctx, observability,
			description, use_cases, common_locations, known_vulns, visual_ids,
			countermeasures, refs, created_at, updated_at
		FROM profiles WHERE id = ? AND deleted = 0`, id,
	).Scan(
		&p.ID, &p.Name, &p.Category, &p.Manufacturer, &p.DeploymentContext,
		&p.Observability, &p.Description, &p.UseCases, &p.CommonLocations,
		&p.KnownVulnerabilities, &p.VisualIdentifiers, &p.Countermeasures,
		&p.References, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// ListProfiles returns a filtered, paginated list of profiles.
func (db *DB) ListProfiles(f models.ProfileFilter) ([]models.Profile, models.Pagination, error) {
	var (
		where  []string
		args   []interface{}
		pag    models.Pagination
	)

	where = append(where, "p.deleted = 0")

	if f.Search != "" {
		where = append(where, "(p.name LIKE ? OR p.description LIKE ? OR p.manufacturer LIKE ?)")
		s := "%" + f.Search + "%"
		args = append(args, s, s, s)
	}
	if f.Category != "" {
		where = append(where, "p.category = ?")
		args = append(args, f.Category)
	}
	if f.Manufacturer != "" {
		where = append(where, "p.manufacturer = ?")
		args = append(args, f.Manufacturer)
	}
	if f.DeploymentContext != "" {
		where = append(where, "p.deployment_ctx = ?")
		args = append(args, f.DeploymentContext)
	}
	if f.Observability != "" {
		where = append(where, "p.observability = ?")
		args = append(args, f.Observability)
	}
	for _, v := range f.Vulnerabilities {
		if v != "" {
			where = append(where, "p.known_vulns LIKE ?")
			args = append(args, "%"+v+"%")
		}
	}

	whereClause := strings.Join(where, " AND ")

	// Count total
	var total int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM profiles p WHERE "+whereClause, args...).Scan(&total)
	if err != nil {
		return nil, pag, err
	}

	pag.TotalItems = total
	pag.PageSize = f.PageSize
	if pag.PageSize < 1 {
		pag.PageSize = 50
	}
	pag.TotalPages = int(math.Ceil(float64(total) / float64(pag.PageSize)))
	pag.CurrentPage = f.Page
	if pag.CurrentPage < 1 {
		pag.CurrentPage = 1
	}
	if pag.CurrentPage > pag.TotalPages && pag.TotalPages > 0 {
		pag.CurrentPage = pag.TotalPages
	}
	pag.HasPrev = pag.CurrentPage > 1
	pag.HasNext = pag.CurrentPage < pag.TotalPages

	offset := (pag.CurrentPage - 1) * pag.PageSize
	args = append(args, pag.PageSize, offset)

	rows, err := db.conn.Query(`
		SELECT p.id, p.name, p.category, p.manufacturer, p.deployment_ctx,
			p.observability, p.description, p.use_cases, p.common_locations,
			p.known_vulns, p.visual_ids, p.countermeasures, p.refs,
			p.created_at, p.updated_at
		FROM profiles p
		WHERE `+whereClause+`
		ORDER BY p.updated_at DESC
		LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, pag, err
	}
	defer rows.Close()

	var profiles []models.Profile
	for rows.Next() {
		var p models.Profile
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Category, &p.Manufacturer, &p.DeploymentContext,
			&p.Observability, &p.Description, &p.UseCases, &p.CommonLocations,
			&p.KnownVulnerabilities, &p.VisualIdentifiers, &p.Countermeasures,
			&p.References, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, pag, err
		}
		profiles = append(profiles, p)
	}
	return profiles, pag, rows.Err()
}

// ListManufacturers returns all distinct manufacturer values for filter population.
func (db *DB) ListManufacturers() ([]string, error) {
	rows, err := db.conn.Query(`
		SELECT DISTINCT manufacturer FROM profiles
		WHERE deleted = 0 AND manufacturer != ''
		ORDER BY manufacturer`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// SearchProfiles returns profiles matching a search term (for autocomplete/dropdowns).
func (db *DB) SearchProfiles(term string, limit int) ([]models.Profile, error) {
	if limit < 1 {
		limit = 20
	}
	rows, err := db.conn.Query(`
		SELECT id, name, category, manufacturer
		FROM profiles
		WHERE deleted = 0 AND (name LIKE ? OR manufacturer LIKE ? OR category LIKE ?)
		ORDER BY name
		LIMIT ?`,
		"%"+term+"%", "%"+term+"%", "%"+term+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var profiles []models.Profile
	for rows.Next() {
		var p models.Profile
		if err := rows.Scan(&p.ID, &p.Name, &p.Category, &p.Manufacturer); err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// AllProfilesForDropdown returns minimal profile data for the pin creation dropdown.
func (db *DB) AllProfilesForDropdown() ([]models.Profile, error) {
	rows, err := db.conn.Query(`
		SELECT id, name, category, manufacturer
		FROM profiles WHERE deleted = 0
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var profiles []models.Profile
	for rows.Next() {
		var p models.Profile
		if err := rows.Scan(&p.ID, &p.Name, &p.Category, &p.Manufacturer); err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// --- Image Operations ---

// CreateImage inserts a new image record.
func (db *DB) CreateImage(img *models.Image) (int64, error) {
	res, err := db.conn.Exec(`
		INSERT INTO images (profile_id, filename, orig_name, caption, mime_type, size_bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		img.ProfileID, img.Filename, img.OrigName, img.Caption,
		img.MimeType, img.SizeBytes, time.Now().UTC(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetImage retrieves an image by ID.
func (db *DB) GetImage(id int64) (*models.Image, error) {
	img := &models.Image{}
	err := db.conn.QueryRow(`
		SELECT id, profile_id, filename, orig_name, caption, mime_type, size_bytes, created_at
		FROM images WHERE id = ?`, id,
	).Scan(&img.ID, &img.ProfileID, &img.Filename, &img.OrigName,
		&img.Caption, &img.MimeType, &img.SizeBytes, &img.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return img, err
}

// ListImagesByProfile returns all images for a given profile.
func (db *DB) ListImagesByProfile(profileID int64) ([]models.Image, error) {
	rows, err := db.conn.Query(`
		SELECT id, profile_id, filename, orig_name, caption, mime_type, size_bytes, created_at
		FROM images WHERE profile_id = ?
		ORDER BY created_at`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var images []models.Image
	for rows.Next() {
		var img models.Image
		if err := rows.Scan(&img.ID, &img.ProfileID, &img.Filename, &img.OrigName,
			&img.Caption, &img.MimeType, &img.SizeBytes, &img.CreatedAt); err != nil {
			return nil, err
		}
		images = append(images, img)
	}
	return images, rows.Err()
}

// GetFirstImageForProfile returns the first image for a profile (for thumbnails).
func (db *DB) GetFirstImageForProfile(profileID int64) (*models.Image, error) {
	img := &models.Image{}
	err := db.conn.QueryRow(`
		SELECT id, profile_id, filename, orig_name, caption, mime_type, size_bytes, created_at
		FROM images WHERE profile_id = ?
		ORDER BY created_at LIMIT 1`, profileID,
	).Scan(&img.ID, &img.ProfileID, &img.Filename, &img.OrigName,
		&img.Caption, &img.MimeType, &img.SizeBytes, &img.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return img, err
}

// DeleteImage removes an image record by ID and returns the filename for disk cleanup.
func (db *DB) DeleteImage(id int64) (string, int64, error) {
	var filename string
	var profileID int64
	err := db.conn.QueryRow(`SELECT filename, profile_id FROM images WHERE id = ?`, id).Scan(&filename, &profileID)
	if err != nil {
		return "", 0, err
	}
	_, err = db.conn.Exec(`DELETE FROM images WHERE id = ?`, id)
	return filename, profileID, err
}

// --- Pin Operations ---

// CreatePin inserts a new map pin and returns its ID.
func (db *DB) CreatePin(pin *models.Pin) (int64, error) {
	now := time.Now().UTC()
	res, err := db.conn.Exec(`
		INSERT INTO pins (profile_id, latitude, longitude, location_label, notes,
			date_observed, fingerprint, submitted_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pin.ProfileID, pin.Latitude, pin.Longitude, pin.LocationLabel,
		pin.Notes, pin.DateObserved, pin.Fingerprint, now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeletePin performs a soft delete on a pin.
func (db *DB) DeletePin(id int64) error {
	_, err := db.conn.Exec(`UPDATE pins SET deleted = 1, updated_at = datetime('now') WHERE id = ?`, id)
	return err
}

// ListPins returns all non-deleted pins, optionally filtered.
func (db *DB) ListPins(f models.PinFilter) ([]models.Pin, error) {
	var where []string
	var args []interface{}

	where = append(where, "pins.deleted = 0")

	if f.ProfileID > 0 {
		where = append(where, "pins.profile_id = ?")
		args = append(args, f.ProfileID)
	}
	if f.Category != "" {
		where = append(where, "p.category = ?")
		args = append(args, f.Category)
	}
	if f.Manufacturer != "" {
		where = append(where, "p.manufacturer = ?")
		args = append(args, f.Manufacturer)
	}

	whereClause := strings.Join(where, " AND ")

	rows, err := db.conn.Query(`
		SELECT pins.id, pins.profile_id, pins.latitude, pins.longitude,
			pins.location_label, pins.notes, pins.date_observed, pins.submitted_at,
			p.name, p.category
		FROM pins
		JOIN profiles p ON p.id = pins.profile_id
		WHERE `+whereClause+`
		ORDER BY pins.submitted_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pins []models.Pin
	for rows.Next() {
		var pin models.Pin
		if err := rows.Scan(
			&pin.ID, &pin.ProfileID, &pin.Latitude, &pin.Longitude,
			&pin.LocationLabel, &pin.Notes, &pin.DateObserved, &pin.SubmittedAt,
			&pin.ProfileName, &pin.ProfileCategory,
		); err != nil {
			return nil, err
		}
		pins = append(pins, pin)
	}
	return pins, rows.Err()
}

// ListPinsByProfile returns all pins for a specific profile (for mini-map).
func (db *DB) ListPinsByProfile(profileID int64) ([]models.Pin, error) {
	return db.ListPins(models.PinFilter{ProfileID: profileID})
}

// --- Federation Sync Endpoints (FRD Section 5.1) ---

// ProfilesSince returns all profiles modified after the given timestamp.
func (db *DB) ProfilesSince(since time.Time, page, pageSize int) ([]models.Profile, error) {
	if pageSize < 1 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}

	rows, err := db.conn.Query(`
		SELECT id, name, category, manufacturer, deployment_ctx, observability,
			description, use_cases, common_locations, known_vulns, visual_ids,
			countermeasures, refs, created_at, updated_at
		FROM profiles
		WHERE updated_at > ? AND deleted = 0
		ORDER BY updated_at
		LIMIT ? OFFSET ?`, since, pageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profiles []models.Profile
	for rows.Next() {
		var p models.Profile
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Category, &p.Manufacturer, &p.DeploymentContext,
			&p.Observability, &p.Description, &p.UseCases, &p.CommonLocations,
			&p.KnownVulnerabilities, &p.VisualIdentifiers, &p.Countermeasures,
			&p.References, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// PinsSince returns all pins modified after the given timestamp.
func (db *DB) PinsSince(since time.Time, page, pageSize int) ([]models.Pin, error) {
	if pageSize < 1 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}

	rows, err := db.conn.Query(`
		SELECT id, profile_id, latitude, longitude, location_label, notes,
			date_observed, submitted_at, updated_at
		FROM pins
		WHERE updated_at > ? AND deleted = 0
		ORDER BY updated_at
		LIMIT ? OFFSET ?`, since, pageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pins []models.Pin
	for rows.Next() {
		var pin models.Pin
		if err := rows.Scan(
			&pin.ID, &pin.ProfileID, &pin.Latitude, &pin.Longitude,
			&pin.LocationLabel, &pin.Notes, &pin.DateObserved,
			&pin.SubmittedAt, &pin.UpdatedAt,
		); err != nil {
			return nil, err
		}
		pins = append(pins, pin)
	}
	return pins, rows.Err()
}

// Stats returns basic database statistics.
func (db *DB) Stats() (profiles, pins, images int, err error) {
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM profiles WHERE deleted = 0`).Scan(&profiles)
	if err != nil {
		return
	}
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM pins WHERE deleted = 0`).Scan(&pins)
	if err != nil {
		return
	}
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM images`).Scan(&images)
	return
}

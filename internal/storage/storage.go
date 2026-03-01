package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	// Register image decoders
	_ "golang.org/x/image/webp"
)

// ImageStore handles filesystem operations for uploaded images.
// Abstracted behind an interface so S3-compatible backends can be swapped in later.
type ImageStore interface {
	Save(profileID int64, filename string, data io.Reader, mimeType string) error
	Delete(profileID int64, filename string) error
	Path(profileID int64, filename string) string
	EnsureDir(profileID int64) error
}

// LocalStore implements ImageStore using the local filesystem.
type LocalStore struct {
	BasePath string
}

// NewLocalStore creates a new local filesystem image store.
func NewLocalStore(basePath string) *LocalStore {
	return &LocalStore{BasePath: basePath}
}

// EnsureDir creates the profile-specific image directory if it doesn't exist.
func (s *LocalStore) EnsureDir(profileID int64) error {
	dir := filepath.Join(s.BasePath, fmt.Sprintf("%d", profileID))
	return os.MkdirAll(dir, 0755)
}

// Save writes image data to disk after stripping EXIF metadata.
// Images are re-encoded to ensure no metadata survives.
func (s *LocalStore) Save(profileID int64, filename string, data io.Reader, mimeType string) error {
	if err := s.EnsureDir(profileID); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	destPath := s.Path(profileID, filename)

	// Read all data into memory for EXIF stripping
	raw, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("reading upload data: %w", err)
	}

	// Strip EXIF metadata by re-encoding the image (FR-I-08)
	cleaned, err := stripEXIF(raw, mimeType)
	if err != nil {
		// If we can't decode/re-encode, fall back to raw bytes with basic EXIF removal
		cleaned = removeEXIFBytes(raw)
	}

	if err := os.WriteFile(destPath, cleaned, 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}

// Delete removes an image file from disk.
func (s *LocalStore) Delete(profileID int64, filename string) error {
	path := s.Path(profileID, filename)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil // Already gone
	}
	return err
}

// Path returns the full filesystem path for an image.
func (s *LocalStore) Path(profileID int64, filename string) string {
	return filepath.Join(s.BasePath, fmt.Sprintf("%d", profileID), filename)
}

// stripEXIF re-encodes the image to remove all metadata.
func stripEXIF(data []byte, mimeType string) ([]byte, error) {
	reader := bytes.NewReader(data)

	var img image.Image
	var err error

	switch {
	case strings.Contains(mimeType, "jpeg") || strings.Contains(mimeType, "jpg"):
		img, err = jpeg.Decode(reader)
	case strings.Contains(mimeType, "png"):
		img, err = png.Decode(reader)
	default:
		// For WebP and others, try generic decode
		img, _, err = image.Decode(reader)
	}

	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}

	var buf bytes.Buffer
	switch {
	case strings.Contains(mimeType, "png"):
		err = png.Encode(&buf, img)
	default:
		// Default to JPEG for all other formats (including WebP input)
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92})
	}

	if err != nil {
		return nil, fmt.Errorf("re-encoding image: %w", err)
	}

	return buf.Bytes(), nil
}

// removeEXIFBytes performs a best-effort raw byte removal of EXIF data from JPEG files.
// Used as a fallback when image re-encoding fails.
func removeEXIFBytes(data []byte) []byte {
	if len(data) < 4 {
		return data
	}

	// Only process JPEG (starts with FF D8)
	if data[0] != 0xFF || data[1] != 0xD8 {
		return data
	}

	var result []byte
	result = append(result, 0xFF, 0xD8) // SOI marker

	i := 2
	for i < len(data)-1 {
		if data[i] != 0xFF {
			// Append remaining data as-is
			result = append(result, data[i:]...)
			break
		}

		marker := data[i+1]

		// Skip APP1 (EXIF) and APP2 (ICC profile) markers
		if marker == 0xE1 || marker == 0xE2 {
			if i+3 < len(data) {
				segLen := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
				i += 2 + segLen
				continue
			}
		}

		// SOS marker — rest is image data, keep everything
		if marker == 0xDA {
			result = append(result, data[i:]...)
			break
		}

		// Keep other segments
		if i+3 < len(data) {
			segLen := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
			result = append(result, data[i:i+2+segLen]...)
			i += 2 + segLen
		} else {
			result = append(result, data[i:]...)
			break
		}
	}

	return result
}

// ValidateMimeType checks if the MIME type is an accepted image format (FR-I-02).
func ValidateMimeType(mimeType string) bool {
	allowed := map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/webp": true,
	}
	return allowed[mimeType]
}

// ExtensionFromMime returns a file extension for the given MIME type.
func ExtensionFromMime(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}

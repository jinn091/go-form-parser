package formparser

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-playground/form/v4"
	"github.com/go-playground/validator/v10"
)

// UploadedFile represents an uploaded file, including its metadata and content.
type UploadedFile struct {
	Filename    string // Original name of the uploaded file
	ContentType string // MIME type of the file (e.g., image/jpeg)
	Content     []byte // Actual file data in bytes
	Hash        string // SHA-256 hash of the file content for integrity checking
}

// Config contains the dependencies and configurations needed for parsing and validating forms.
type Config struct {
	Decoder            *form.Decoder            // Form decoder to map form data into a Go struct
	Validator          *validator.Validate      // Validator for struct validation
	FieldErrorMessages map[string]string        // Custom error messages for specific fields
	Files              map[string]*UploadedFile // Uploaded files, accessible by field name
}

// ParseFormBasedOnContentType determines the request's content type and dispatches
// to the appropriate parser (either for URL-encoded forms or multipart forms with files).
func (cfg *Config) ParseFormBasedOnContentType(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	contentType := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(contentType, "multipart/form-data"):
		return cfg.parseMultipart(w, r, dst)
	case strings.HasPrefix(contentType, "application/x-www-form-urlencoded"):
		return cfg.parseURLEncoded(w, r, dst)
	default:
		http.Error(w, "Unsupported Content-Type", http.StatusUnsupportedMediaType)
		return errors.New("unsupported content type")
	}
}

// parseURLEncoded handles forms submitted as "application/x-www-form-urlencoded".
// It parses the form, decodes it into the provided struct, and validates the result.
func (cfg *Config) parseURLEncoded(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Can't parse form", http.StatusBadRequest)
		return err
	}
	_ = cfg.Decoder.Decode(dst, r.PostForm)
	return cfg.validateAndRespond(w, dst)
}

// parseMultipart handles "multipart/form-data" forms, which can contain both form fields and file uploads.
// Files are read into memory, validated, and saved into cfg.Files. Form fields and file hashes are decoded into the struct.
func (cfg *Config) parseMultipart(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Can't parse multipart", http.StatusBadRequest)
		return err
	}

	values := make(url.Values)
	cfg.Files = make(map[string]*UploadedFile)

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		defer part.Close()

		formName := part.FormName()
		if part.FileName() == "" {
			// Regular form field (not a file)
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(part)
			values.Add(formName, buf.String())
			continue
		}

		contentType := part.Header.Get("Content-Type")
		if !isAllowedContentType(contentType) {
			http.Error(w, "Unsupported file type", http.StatusBadRequest)
			return fmt.Errorf("unsupported file type: %s", contentType)
		}

		const maxFileSize = 5 << 20 // Limit file size to 5MB
		var fileBuf bytes.Buffer
		n, err := io.CopyN(&fileBuf, part, maxFileSize+1)
		if err != nil && err != io.EOF {
			http.Error(w, "Error reading file", http.StatusInternalServerError)
			return err
		}
		if n > maxFileSize {
			http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
			return fmt.Errorf("file too large: %d bytes", n)
		}

		content := fileBuf.Bytes()
		hash := sha256.Sum256(content)

		// Save file metadata and content
		cfg.Files[formName] = &UploadedFile{
			Filename:    part.FileName(),
			ContentType: contentType,
			Content:     content,
			Hash:        fmt.Sprintf("%x", hash),
		}

		// Use the file hash as the field value when decoding into struct
		values.Add(formName, fmt.Sprintf("%x", hash))
	}

	_ = cfg.Decoder.Decode(dst, values)
	return cfg.validateAndRespond(w, dst)
}

// validateAndRespond validates the decoded struct. If validation fails,
// it responds with a JSON error message containing the field errors.
func (cfg *Config) validateAndRespond(w http.ResponseWriter, dst interface{}) error {
	if err := cfg.Validator.Struct(dst); err != nil {
		if validationErrs, ok := err.(validator.ValidationErrors); ok {
			fieldErrors := make(map[string]string)
			for _, ve := range validationErrs {
				field := strings.ToLower(ve.Field())
				if msg, exists := cfg.FieldErrorMessages[field]; exists {
					fieldErrors[field] = msg
				} else {
					fieldErrors[field] = fmt.Sprintf("%s is %s", field, ve.Tag())
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": "Validation failed",
				"fields":  fieldErrors,
			})
			return err
		}
		http.Error(w, "Validation failed", http.StatusBadRequest)
		return err
	}
	return nil
}

// isAllowedContentType checks if the file's content type is one of the accepted types.
// Helps prevent users from uploading disallowed or dangerous files.
func isAllowedContentType(contentType string) bool {
	allowed := []string{"image/jpeg", "image/png", "application/pdf"}
	for _, a := range allowed {
		if contentType == a {
			return true
		}
	}
	return false
}

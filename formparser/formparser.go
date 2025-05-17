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

// UploadedFile holds metadata and content of a parsed uploaded file.
// You can access uploaded files from `Config.Files["fieldname"]`.
type UploadedFile struct {
	Filename    string // original filename (e.g. "photo.jpg")
	ContentType string // MIME type (e.g. "image/jpeg")
	Content     []byte // file data as bytes
	Hash        string // SHA-256 checksum of file content
}

// Config defines the shared parser config and context.
// Pass it to your handler once and reuse it across requests.
type Config struct {
	Decoder            *form.Decoder            // required: form decoder
	Validator          *validator.Validate      // required: struct validator
	FieldErrorMessages map[string]string        // optional: map[json|form field]custom message
	Files              map[string]*UploadedFile // output: populated if multipart files are parsed
}

// ParseFormBasedOnContentType detects the Content-Type and routes parsing accordingly.
// Supports: multipart/form-data, application/x-www-form-urlencoded, application/json
//
// Params:
//   - w: http.ResponseWriter — for sending errors
//   - r: *http.Request — the incoming HTTP request
//   - dst: interface{} — a pointer to a struct to decode the parsed form/JSON data into
//
// Returns: error if any parsing or validation fails
func (cfg *Config) ParseFormBasedOnContentType(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	contentType := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(contentType, "multipart/form-data"):
		return cfg.parseMultipart(w, r, dst)
	case strings.HasPrefix(contentType, "application/x-www-form-urlencoded"):
		return cfg.parseURLEncoded(w, r, dst)
	case strings.HasPrefix(contentType, "application/json"):
		return cfg.parseJSON(w, r, dst)
	default:
		http.Error(w, "Unsupported Content-Type", http.StatusUnsupportedMediaType)
		return errors.New("unsupported content type")
	}
}

// parseJSON decodes JSON request body into a struct and validates it.
// Use this for `Content-Type: application/json`
func (cfg *Config) parseJSON(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return err
	}
	return cfg.validateAndRespond(w, dst)
}

// parseURLEncoded handles `application/x-www-form-urlencoded` payloads.
// Populates the dst struct and validates it.
func (cfg *Config) parseURLEncoded(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Can't parse form", http.StatusBadRequest)
		return err
	}
	_ = cfg.Decoder.Decode(dst, r.PostForm)
	return cfg.validateAndRespond(w, dst)
}

// parseMultipart handles `multipart/form-data` requests and stores uploaded file data in memory.
// Populates `Config.Files` with file metadata and inserts file hashes into `dst` struct if matching field exists.
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
			// regular field
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(part)
			values.Add(formName, buf.String())
			continue
		}

		// validate file content type
		contentType := part.Header.Get("Content-Type")
		if !isAllowedContentType(contentType) {
			http.Error(w, "Unsupported file type", http.StatusBadRequest)
			return fmt.Errorf("unsupported file type: %s", contentType)
		}

		// enforce max file size (5MB)
		const maxFileSize = 5 << 20
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

		// store file metadata
		cfg.Files[formName] = &UploadedFile{
			Filename:    part.FileName(),
			ContentType: contentType,
			Content:     content,
			Hash:        fmt.Sprintf("%x", hash),
		}

		// insert hash as string field into struct
		values.Add(formName, fmt.Sprintf("%x", hash))
	}

	_ = cfg.Decoder.Decode(dst, values)
	return cfg.validateAndRespond(w, dst)
}

// validateAndRespond uses go-playground/validator to validate the decoded struct.
// If validation fails, it sends a structured JSON error with all field issues.
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

// isAllowedContentType restricts accepted uploaded file MIME types.
// You can extend this list to support more types.
func isAllowedContentType(contentType string) bool {
	allowed := []string{"image/jpeg", "image/png", "application/pdf"}
	for _, a := range allowed {
		if contentType == a {
			return true
		}
	}
	return false
}

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
type UploadedFile struct {
	Filename    string
	ContentType string
	Content     []byte
	Hash        string
}

// Config defines the shared parser config and context.
type Config struct {
	Decoder            *form.Decoder
	Validator          *validator.Validate
	FieldErrorMessages map[string]string
	Files              map[string]*UploadedFile
	AllowedMIMETypes   []string // Optional: user-defined MIME type whitelist
	MaxFileSize        int64    // Optional: max size per file in bytes (default 5MB)
}

// ParseFormBasedOnContentType routes to JSON, URL-encoded, or multipart parser.
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

// parseJSON handles JSON payload.
func (cfg *Config) parseJSON(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return err
	}
	return cfg.validateAndRespond(w, dst)
}

// parseURLEncoded handles application/x-www-form-urlencoded data.
func (cfg *Config) parseURLEncoded(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Can't parse form", http.StatusBadRequest)
		return err
	}
	_ = cfg.Decoder.Decode(dst, r.PostForm)
	return cfg.validateAndRespond(w, dst)
}

// parseMultipart handles multipart/form-data and stores uploaded files.
func (cfg *Config) parseMultipart(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Can't parse multipart", http.StatusBadRequest)
		return err
	}

	values := make(url.Values)
	cfg.Files = make(map[string]*UploadedFile)

	if cfg.MaxFileSize == 0 {
		cfg.MaxFileSize = 5 << 20 // default 5MB
	}

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		defer part.Close()

		formName := part.FormName()

		if part.FileName() == "" {
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(part)
			values.Add(formName, buf.String())
			continue
		}

		contentType := part.Header.Get("Content-Type")
		if !cfg.isAllowedContentType(contentType) {
			http.Error(w, "Unsupported file type", http.StatusBadRequest)
			return fmt.Errorf("unsupported file type: %s", contentType)
		}

		var fileBuf bytes.Buffer
		n, err := io.CopyN(&fileBuf, part, cfg.MaxFileSize+1)
		if err != nil && err != io.EOF {
			http.Error(w, "Error reading file", http.StatusInternalServerError)
			return err
		}
		if n > cfg.MaxFileSize {
			http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
			return fmt.Errorf("file too large: %d bytes", n)
		}

		content := fileBuf.Bytes()
		hash := sha256.Sum256(content)

		cfg.Files[formName] = &UploadedFile{
			Filename:    part.FileName(),
			ContentType: contentType,
			Content:     content,
			Hash:        fmt.Sprintf("%x", hash),
		}

		values.Add(formName, fmt.Sprintf("%x", hash))
	}

	_ = cfg.Decoder.Decode(dst, values)
	return cfg.validateAndRespond(w, dst)
}

// validateAndRespond validates the dst struct and returns JSON error if failed.
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

// isAllowedContentType checks against user-defined or default MIME types.
func (cfg *Config) isAllowedContentType(contentType string) bool {
	// No allowed MIME types = no files allowed
	if len(cfg.AllowedMIMETypes) == 0 {
		return false
	}
	for _, allowed := range cfg.AllowedMIMETypes {
		if contentType == allowed {
			return true
		}
	}
	return false
}

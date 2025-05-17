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

// Config holds shared dependencies and config options for the parser.
type Config struct {
	Decoder            *form.Decoder
	Validator          *validator.Validate
	FieldErrorMessages map[string]string // Optional: field-specific validation messages
}

// ────────────────────────────────────────────────────────────────
// 📌 Main entry point for parsing and validating forms
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

// ────────────────────────────────────────────────────────────────
// 🔤 Handles application/x-www-form-urlencoded data
func (cfg *Config) parseURLEncoded(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Can't parse form", http.StatusBadRequest)
		return err
	}
	_ = cfg.Decoder.Decode(dst, r.PostForm)
	return cfg.validateAndRespond(w, dst)
}

// ────────────────────────────────────────────────────────────────
// 📎 Handles multipart/form-data with file validation
func (cfg *Config) parseMultipart(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Can't parse multipart", http.StatusBadRequest)
		return err
	}

	values := make(url.Values)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		defer part.Close()

		if part.FileName() == "" {
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(part)
			values.Add(part.FormName(), buf.String())
			continue
		}

		contentType := part.Header.Get("Content-Type")
		if !isAllowedContentType(contentType) {
			http.Error(w, "Unsupported file type", http.StatusBadRequest)
			return fmt.Errorf("unsupported file type: %s", contentType)
		}

		const maxFileSize = 5 << 20 // 5MB
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

		hash := sha256.Sum256(fileBuf.Bytes())
		values.Add(part.FormName(), fmt.Sprintf("%x", hash))
	}

	_ = cfg.Decoder.Decode(dst, values)
	return cfg.validateAndRespond(w, dst)
}

// ────────────────────────────────────────────────────────────────
// ✅ Validates parsed struct fields using validator instance
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
			// Respond with JSON error
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":   "Validation failed",
				"details": fieldErrors,
			})
			return err
		}

		http.Error(w, "Validation failed", http.StatusBadRequest)
		return err
	}
	return nil
}

// ────────────────────────────────────────────────────────────────
// 📁 File type whitelist
func isAllowedContentType(contentType string) bool {
	allowed := []string{"image/jpeg", "image/png", "application/pdf"}
	for _, a := range allowed {
		if contentType == a {
			return true
		}
	}
	return false
}

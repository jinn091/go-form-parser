package formparser

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-playground/form/v4"
	"github.com/go-playground/validator/v10"
)

// Config holds shared dependencies such as a form decoder and a validator instance.
type Config struct {
	Decoder   *form.Decoder
	Validator *validator.Validate
}

// ────────────────────────────────────────────────────────────────
// 📌 Main entry point for parsing and validating forms
//
// Detects the request's Content-Type and chooses appropriate handler.
//
// 🧪 Example curl for x-www-form-urlencoded:
//
//	curl -X POST http://localhost:8080/register \
//	  -H "Content-Type: application/x-www-form-urlencoded" \
//	  -d "name=Ko Nyi&email=ko@example.com&age=25"
//
// 🧪 Example curl for multipart/form-data with file:
//
//	curl -X POST http://localhost:8080/register \
//	  -F "name=Ko Nyi" -F "email=ko@example.com" -F "age=25" \
//	  -F "profile_picture=@path/to/image.jpg"
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
//
// Simple parsing of URL-encoded form data (like HTML forms without files).
//
// Example curl:
//
//	curl -X POST http://localhost:8080/register \
//	  -H "Content-Type: application/x-www-form-urlencoded" \
//	  -d "name=Alice&email=alice@example.com&age=22"
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
//
// Parses form fields and files, supports max file size and type checking.
// Accepted types: JPEG, PNG, PDF; Max file size: 5MB
//
// Example curl:
//
//	curl -X POST http://localhost:8080/register \
//	  -F "name=John" -F "email=john@example.com" -F "age=30" \
//	  -F "profile_picture=@path/to/file.jpg"
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
			// Handle normal field
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(part)
			values.Add(part.FormName(), buf.String())
			continue
		}

		// File: validate type
		contentType := part.Header.Get("Content-Type")
		if !isAllowedContentType(contentType) {
			http.Error(w, "Unsupported file type", http.StatusBadRequest)
			return fmt.Errorf("unsupported file type: %s", contentType)
		}

		// File: validate size (max 5MB)
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

		// File: SHA-256 hash
		hash := sha256.Sum256(fileBuf.Bytes())
		values.Add(part.FormName(), fmt.Sprintf("%x", hash))
	}

	_ = cfg.Decoder.Decode(dst, values)
	return cfg.validateAndRespond(w, dst)
}

// ────────────────────────────────────────────────────────────────
// ✅ Validates parsed struct fields using validator instance
//
// Called after form data has been decoded into the destination struct.
func (cfg *Config) validateAndRespond(w http.ResponseWriter, dst interface{}) error {
	if err := cfg.Validator.Struct(dst); err != nil {
		http.Error(w, "Validation failed: "+err.Error(), http.StatusBadRequest)
		return err
	}
	return nil
}

// ────────────────────────────────────────────────────────────────
// 📁 File type whitelist
//
// List of accepted MIME types for uploaded files.
// Extendable based on needs.
func isAllowedContentType(contentType string) bool {
	allowed := []string{"image/jpeg", "image/png", "application/pdf"}
	for _, a := range allowed {
		if contentType == a {
			return true
		}
	}
	return false
}

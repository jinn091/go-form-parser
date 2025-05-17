package test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"

	"github.com/go-playground/form/v4"
	"github.com/go-playground/validator/v10"
	"github.com/jinn091/go-form-parser/formparser"
	"github.com/stretchr/testify/assert"
)

type TestForm struct {
	Name  string `form:"name" validate:"required"`
	Email string `form:"email" validate:"required,email"`
}

func setupParser() *formparser.Config {
	return &formparser.Config{
		Decoder:   form.NewDecoder(),
		Validator: validator.New(),
		FieldErrorMessages: map[string]string{
			"name":  "Name is required",
			"email": "Invalid email address",
		},
		AllowedMIMETypes: []string{"image/png"},
		MaxFileSize:      1024 * 1024, // 1MB
	}
}

func TestParseJSON(t *testing.T) {
	cfg := setupParser()
	payload := `{"name":"John","email":"john@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	var form TestForm
	err := cfg.ParseFormBasedOnContentType(w, req, &form)

	assert.NoError(t, err)
	assert.Equal(t, "John", form.Name)
	assert.Equal(t, "john@example.com", form.Email)
}

func TestParseURLEncoded(t *testing.T) {
	cfg := setupParser()
	data := url.Values{}
	data.Set("name", "Jane")
	data.Set("email", "jane@example.com")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	var form TestForm
	err := cfg.ParseFormBasedOnContentType(w, req, &form)

	assert.NoError(t, err)
	assert.Equal(t, "Jane", form.Name)
	assert.Equal(t, "jane@example.com", form.Email)
}

func TestParseMultipart(t *testing.T) {
	cfg := setupParser()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = writer.WriteField("name", "Alice")
	_ = writer.WriteField("email", "alice@example.com")

	// Custom file part with explicit Content-Type
	partHeaders := textproto.MIMEHeader{}
	partHeaders.Set("Content-Disposition", `form-data; name="avatar"; filename="avatar.png"`)
	partHeaders.Set("Content-Type", "image/png")

	fileWriter, err := writer.CreatePart(partHeaders)
	assert.NoError(t, err)

	content := []byte("PNG IMAGE CONTENT")
	_, _ = fileWriter.Write(content)
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	var form TestForm
	err = cfg.ParseFormBasedOnContentType(w, req, &form)

	assert.NoError(t, err)
	assert.Equal(t, "Alice", form.Name)
	assert.Equal(t, "alice@example.com", form.Email)

	file := cfg.Files["avatar"]
	assert.NotNil(t, file)
	assert.Equal(t, "avatar.png", file.Filename)
	assert.Equal(t, "image/png", file.ContentType)
	assert.Equal(t, content, file.Content)

	expectedHash := sha256.Sum256(content)
	assert.Equal(t, fmt.Sprintf("%x", expectedHash), file.Hash)
}

func TestValidationError(t *testing.T) {
	cfg := setupParser()

	payload := `{"name":"", "email":"invalid-email"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	var form TestForm
	err := cfg.ParseFormBasedOnContentType(w, req, &form)

	assert.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)

	var response map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&response)
	assert.Equal(t, "Validation failed", response["message"])
}

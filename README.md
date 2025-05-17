# 🧾 formparser

`formparser` is a lightweight Go library that helps you parse and validate form data from HTTP requests — including support for `application/json`, `application/x-www-form-urlencoded`, and `multipart/form-data` with file validation (type and size).

---

## ✨ Features

- ✅ Parses HTML and JSON form data into Go structs
- ✅ Supports `application/json`, `application/x-www-form-urlencoded`, and `multipart/form-data`
- ✅ Validates input using [`validator`](https://github.com/go-playground/validator)
- ✅ Rejects unsupported MIME types (JPEG, PNG, PDF by default)
- ✅ Limits file uploads by size (default: 5MB)
- ✅ Collects uploaded file content so you can save them manually (in memory)

---

## 📦 Installation

```bash
go get github.com/jinn091/formparser
```

---

## 🧑‍💻 Usage Example

```go
package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/jinn091/formparser"
	"github.com/go-playground/form/v4"
	"github.com/go-playground/validator/v10"
)

type RegistrationForm struct {
	Name           string `form:"name" json:"name" validate:"required,min=2"`
	Email          string `form:"email" json:"email" validate:"required,email"`
	Age            int    `form:"age" json:"age" validate:"gte=18,lte=100"`
	ProfilePicture string `form:"profile_picture"`
}

func main() {
	cfg := &formparser.Config{
		Decoder:   form.NewDecoder(),
		Validator: validator.New(),
	}

	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		var form RegistrationForm
		if err := cfg.ParseFormBasedOnContentType(w, r, &form); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Optional: save uploaded file manually
		if file := cfg.Files["profile_picture"]; file != nil {
			_ = os.Mkdir("uploads", 0755)
			_ = os.WriteFile("uploads/"+file.Filename, file.Content, 0644)
			fmt.Println("Saved file to uploads/" + file.Filename)
		}

		fmt.Fprintf(w, "Form submitted successfully: %+v", form)
	})

	http.ListenAndServe(":8080", nil)
}
```

---

## 🧪 CURL Testing

### Submit a JSON body:
```bash
curl -X POST http://localhost:8080/register \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice", "email": "alice@example.com", "age": 25}'
```

### Submit a URL-encoded form:
```bash
curl -X POST http://localhost:8080/register \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "name=Alice&email=alice@example.com&age=25"
```

### Submit with file upload:
```bash
curl -X POST http://localhost:8080/register \
  -F "name=Bob" \
  -F "email=bob@example.com" \
  -F "age=30" \
  -F "profile_picture=@./your_image.jpg"
```

---

## 🔐 File Validation

| Check         | Description                        |
|---------------|------------------------------------|
| MIME Type     | Only JPEG, PNG, and PDF accepted   |
| Max File Size | 5 MB (`5 << 20`)                   |
| Output        | SHA-256 hash in the struct field   |
| Manual Save   | Use `cfg.Files["field_name"]`      |

---

## 📃 License

MIT License © [Jinn](https://github.com/jinn091)
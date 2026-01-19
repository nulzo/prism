package domain

import (
	"reflect"
	"strings"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/locales/en"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	en_translations "github.com/go-playground/validator/v10/translations/en"
)

// trans is a private global translator
var trans ut.Translator

// InitValidator configures the validator engine.
// CALL THIS ONCE in your main.go
func InitValidator() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		// 1. Register a function to use the "json" tag name instead of Struct Field Name
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})

		// 2. Setup the Universal Translator (English)
		en := en.New()
		uni := ut.New(en, en)
		trans, _ = uni.GetTranslator("en")

		// 3. Register default translations (this does the magic text generation)
		_ = en_translations.RegisterDefaultTranslations(v, trans)
	}
}

// ParseValidationError converts raw technical errors into a clean map
// Example: "required" -> "email is a required field"
func ParseValidationError(err error) map[string]string {
	errMap := make(map[string]string)

	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		for _, e := range validationErrors {
			// e.Translate uses our global 'trans' to generate the message
			errMap[e.Field()] = e.Translate(trans)
		}
		return errMap
	}

	// Fallback for JSON parsing errors (e.g. sending "abc" for an int field)
	errMap["body"] = "Invalid request body format"
	return errMap
}

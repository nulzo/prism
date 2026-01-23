package validator

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/locales/en"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	en_translations "github.com/go-playground/validator/v10/translations/en"
)

// Validator wraps the translation logic for validation errors.
type Validator struct {
	trans ut.Translator
}

// New configures the validator engine and returns a new Validator instance.
func New() *Validator {
	var trans ut.Translator

	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})

		en := en.New()
		uni := ut.New(en, en)
		trans, _ = uni.GetTranslator("en")

		_ = en_translations.RegisterDefaultTranslations(v, trans)
	}

	return &Validator{
		trans: trans,
	}
}

// ParseError converts raw technical errors into a clean map.
// When defined, nested errors can be resolved into their hierarchical naming.
func (v *Validator) ParseError(err error) map[string]string {
	errMap := make(map[string]string)

	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		for _, e := range validationErrors {
			ns := e.Namespace()

			if i := strings.Index(ns, "."); i != -1 {
				ns = ns[i+1:]
			}

			// If translator is nil (shouldn't happen if initialized), fallback
			var msg string
			if v.trans != nil {
				msg = e.Translate(v.trans)
			} else {
				msg = e.Error()
			}

			if e.Tag() == "oneof" {
				msg = fmt.Sprintf("must be one of [%s]", strings.ReplaceAll(e.Param(), " ", ", "))
			}

			errMap[ns] = msg
		}
		return errMap
	}

	errMap["body"] = "Invalid request body format. Please fix your payload."
	return errMap
}

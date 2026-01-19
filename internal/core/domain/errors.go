package domain

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Problem implements RFC 9457
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`

	Extensions map[string]interface{} `json:"-"`

	Log error `json:"-"`
}

func (p *Problem) Error() string {
	return fmt.Sprintf("[%d] %s: %s", p.Status, p.Title, p.Detail)
}

func (p *Problem) MarshalJSON() ([]byte, error) {
	type Alias Problem

	data := make(map[string]interface{})

	for k, v := range p.Extensions {
		data[k] = v
	}

	stdJSON, _ := json.Marshal(Alias(*p))
	_ = json.Unmarshal(stdJSON, &data)

	return json.Marshal(data)
}

type ProblemOption func(*Problem)

// New creates a generic Problem
func New(status int, title, detail string, opts ...ProblemOption) *Problem {
	p := &Problem{
		Type:       "about:blank", // Default as per RFC
		Title:      title,
		Status:     status,
		Detail:     detail,
		Extensions: make(map[string]interface{}),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// WithExtension adds a custom key-value pair to the response
func WithExtension(key string, value interface{}) ProblemOption {
	return func(p *Problem) {
		p.Extensions[key] = value
	}
}

// WithLog attaches an internal error for server-side logging
func WithLog(err error) ProblemOption {
	return func(p *Problem) {
		p.Log = err
	}
}

// WithType sets the RFC "type" URI
func WithType(uri string) ProblemOption {
	return func(p *Problem) {
		p.Type = uri
	}
}

// AppError defines a standard error shape for the API
type Error struct {
	// HTTP Status Code (e.g., 400, 429, 500)
	Code int
	// Safe message for the client
	Message string
	// Original error for internal logging
	Log error
}

// Error implements standard error interface
func (e *Error) Error() string {
	return e.Message
}

// AppError creates a generic application error
func AppError(code int, message string, err error) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Log:     err,
	}
}

// ValidationError creates a rich validation error
func ValidationError(validationErrors map[string]string) *Problem {
	return New(
		http.StatusBadRequest,
		"Validation Error",
		"One or more fields failed validation",
		// to be RFC compliant we need to fill this out once we get a URL in the config
		func(p *Problem) { p.Type = "https://example.com/probs/validation" },
		// bind with errors
		WithExtension("errors", validationErrors),
	)
}

// BadRequestError creates a standard error for a bad request
func BadRequestError(detail string, opts ...ProblemOption) *Problem {
	return New(http.StatusBadRequest, "Bad Request", detail, opts...)
}

// InternalError creates a standard error for any internal server error
func InternalError(msg string, err error) *Error {
	return &Error{Code: http.StatusInternalServerError, Message: msg, Log: err}
}

// NotFoundError creates a standard 404 error
func NotFoundError(msg string) *Error {
	return &Error{Code: http.StatusNotFound, Message: msg}
}

// UnauthorizedError creates a 401 unauthed error
func UnauthorizedError(msg string) *Error {
	return &Error{Code: http.StatusUnauthorized, Message: msg}
}

// ProviderError creates 502 gateway error for providers
func ProviderError(msg string, err error) *Error {
	// 502 Bad Gateway for upstream AI provider failures
	return &Error{Code: http.StatusBadGateway, Message: msg, Log: err}
}

// RateLimitError creates standard 429 rate limit error
func RateLimitError(msg string) *Error {
	return &Error{Code: http.StatusTooManyRequests, Message: msg}
}

// WrapError allows wrapping a standard error in an AppError
func WrapError(err error, code int, msg string) *Error {
	if err == nil {
		return nil
	}

	return &Error{
		Code:    code,
		Message: fmt.Sprintf("%s: %v", msg, err),
		Log:     err,
	}
}

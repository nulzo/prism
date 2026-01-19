package httpclient

import "fmt"

// UpstreamError represents an error returned by an upstream service
type UpstreamError struct {
	StatusCode int
	Body       []byte
	URL        string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream error: status %d from %s", e.StatusCode, e.URL)
}

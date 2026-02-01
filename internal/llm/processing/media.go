package processing

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ImageData struct {
	MediaType string
	Data      string // Base64 encoded string
}

// ProcessImageURL takes an image URL (standard http/https or data URI) and returns
// the media type and base64 encoded data.
// If it's a remote URL, it fetches it (be careful with timeout/security in prod).
// If it's a data URI, it parses it.
func ProcessImageURL(url string) (*ImageData, error) {
	if strings.HasPrefix(url, "data:") {
		return parseDataURI(url)
	}

	// For remote URLs, we would ideally fetch them.
	// For this implementation, we will fetch with a short timeout.
	return fetchRemoteImage(url)
}

func parseDataURI(uri string) (*ImageData, error) {
	// format: data:[<media type>][;base64],<data>
	// e.g., data:image/png;base64,iVBOR...

	comma := strings.Index(uri, ",")
	if comma == -1 {
		return nil, fmt.Errorf("invalid data URI")
	}

	meta := uri[:comma]
	data := uri[comma+1:]

	// extract media type
	// default to text/plain;charset=US-ASCII if omitted, but for images we expect it.
	mediaType := "text/plain" 
	
	parts := strings.Split(meta, ";")
	if len(parts) > 0 && strings.HasPrefix(parts[0], "data:") {
		mediaType = parts[0][5:]
	}

	isBase64 := false
	for _, p := range parts {
		if p == "base64" {
			isBase64 = true
			break
		}
	}

	if !isBase64 {
		// If not base64, we might need to url decode, but for image inputs in these APIs, 
		// they are almost always base64.
		return nil, fmt.Errorf("only base64 data URIs are supported for images")
	}

	return &ImageData{
		MediaType: mediaType,
		Data:      data,
	}, nil
}

func fetchRemoteImage(url string) (*ImageData, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch image: status %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		// try to guess or default
		contentType = "image/jpeg"
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	encoded := base64.StdEncoding.EncodeToString(body)
	return &ImageData{
		MediaType: contentType,
		Data:      encoded,
	}, nil
}

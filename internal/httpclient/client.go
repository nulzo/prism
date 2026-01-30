package httpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// HTTPClient defines the interface for an HTTP http
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// SendRequest handles the common logic of creating a request, sending it, and checking the status code.
func SendRequest(ctx context.Context, client HTTPClient, method, url string, headers map[string]string, body interface{}, response interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check for non-200 status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return &UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       respBody,
			URL:        url,
		}
	}

	if response != nil {
		if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

type LineProcessor func(line string) error

func StreamRequest(ctx context.Context, client HTTPClient, method, url string, headers map[string]string, body interface{}, processLine LineProcessor) error {
	var bodyReader *bytes.Buffer
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonBody)
	} else {
		bodyReader = bytes.NewBuffer(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("stream request failed: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return &UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       respBody,
			URL:        url,
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		if err := processLine(line); err != nil {
			return err
		}
	}

	return scanner.Err()
}
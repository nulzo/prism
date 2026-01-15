package utils

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// StreamProcessor is a function that takes a raw line from the stream and returns
// a processed result (or nil to skip) and an error.
// If valid data is found, it should be sent to the channel within this function,
// or returned to be sent by the caller. 
// However, to keep it simple: the processor parses the line and returns the object to be sent.
// Actually, generic return types are messy here. 
// Let's use a callback approach: The caller provides a function "OnLine(line string) error"
type LineProcessor func(line string) error

// StreamRequest handles the boilerplate of setting up an HTTP stream, checking status,
// and scanning the response body line-by-line.
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

	// Ensure we close body if we error out early, but otherwise the loop handles it
	// Actually, standard practice: close after function returns, but this function blocks until stream ends.
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("stream api error (status %d)", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		
		// Delegate specific parsing logic to the caller
		if err := processLine(line); err != nil {
			// If the processor returns an error, we abort the stream
			return err
		}
	}

	return scanner.Err()
}

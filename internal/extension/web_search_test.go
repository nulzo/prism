package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nulzo/model-router-api/pkg/api"
)

func TestWebSearchExtension_Identity(t *testing.T) {
	ext := NewWebSearchExtension("")
	if got, want := ext.ID(), "prism:web_search"; got != want {
		t.Errorf("ID: got %q, want %q", got, want)
	}
	// The wire tool name must satisfy OpenAI's function-name schema
	// (^[a-zA-Z0-9_-]{1,64}$) because Gemini's OpenAI-compat shim
	// silently drops tools with illegal names. Colons in the ID get
	// sanitised to underscores.
	if got, want := ext.ToolName(), "prism_web_search"; got != want {
		t.Errorf("ToolName: got %q, want %q", got, want)
	}
	tool, err := ext.BuildTool(api.ExtensionConfig{})
	if err != nil {
		t.Fatalf("BuildTool: %v", err)
	}
	if tool.Function.Name != ext.ToolName() {
		t.Errorf("tool function name: got %q, want %q", tool.Function.Name, ext.ToolName())
	}
}

func TestWebSearchExtension_Execute(t *testing.T) {
	// Create a mock SearXNG server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("expected path '/search', got '%s'", r.URL.Path)
		}

		q := r.URL.Query().Get("q")
		if q == "" {
			t.Error("expected query parameter 'q'")
		}

		format := r.URL.Query().Get("format")
		if format != "json" {
			t.Errorf("expected format 'json', got '%s'", format)
		}

		// Return mock JSON response
		resp := searxngResponse{
			Query: q,
			Results: []struct {
				URL     string `json:"url"`
				Title   string `json:"title"`
				Content string `json:"content"`
			}{
				{URL: "https://example.com/1", Title: "Result 1", Content: "Snippet 1"},
				{URL: "https://example.com/2", Title: "Result 2", Content: "Snippet 2"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	ext := NewWebSearchExtension(mockServer.URL)

	// Test valid query
	args := []byte(`{"query": "test query", "max_results": 2}`)
	res, err := ext.Execute(context.Background(), api.ExtensionConfig{}, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var results []webSearchResult
	if err := json.Unmarshal([]byte(res), &results); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "Result 1" {
		t.Errorf("expected 'Result 1', got '%s'", results[0].Title)
	}

	// Test missing query
	_, err = ext.Execute(context.Background(), api.ExtensionConfig{}, []byte(`{}`))
	if err == nil {
		t.Error("expected error for missing query")
	}
}

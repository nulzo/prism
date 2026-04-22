package extension

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nulzo/model-router-api/pkg/api"
)

func TestDatetimeExtension_Identity(t *testing.T) {
	ext := NewDatetimeExtension()
	if got, want := ext.ID(), "prism:datetime"; got != want {
		t.Errorf("ID: got %q, want %q", got, want)
	}
	if got, want := ext.ToolName(), "prism_datetime"; got != want {
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

func TestDatetimeExtension_Execute(t *testing.T) {
	ext := NewDatetimeExtension()

	// Test with no timezone
	res, err := ext.Execute(context.Background(), api.ExtensionConfig{}, []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(res), &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if _, ok := result["datetime"]; !ok {
		t.Error("expected 'datetime' in result")
	}
	if result["timezone"] != "UTC" {
		t.Errorf("expected timezone 'UTC', got '%s'", result["timezone"])
	}

	// Test with specific timezone
	res, err = ext.Execute(context.Background(), api.ExtensionConfig{}, []byte(`{"timezone": "America/New_York"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := json.Unmarshal([]byte(res), &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Validate the returned timezone string
	loc, _ := time.LoadLocation("America/New_York")
	if result["timezone"] != loc.String() {
		t.Errorf("expected timezone '%s', got '%s'", loc.String(), result["timezone"])
	}
}

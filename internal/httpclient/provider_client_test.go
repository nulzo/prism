package httpclient

import (
	"testing"
	"time"
)

func TestNewRequestClient_SetsTotalTimeout(t *testing.T) {
	client := NewRequestClient(3 * time.Minute)
	if got, want := client.Timeout, 3*time.Minute; got != want {
		t.Fatalf("Timeout = %v, want %v", got, want)
	}
}

func TestNewStreamingClient_DisablesTotalTimeout(t *testing.T) {
	client := NewStreamingClient()
	if client.Timeout != 0 {
		t.Fatalf("Timeout = %v, want 0 for streaming", client.Timeout)
	}
}

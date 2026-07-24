package httpclient

import (
	"net/http"
	"testing"
	"time"
)

func TestNewRequestClient_SetsTotalTimeout(t *testing.T) {
	client := NewRequestClient(3 * time.Minute)
	if got, want := client.Timeout, 3*time.Minute; got != want {
		t.Fatalf("Timeout = %v, want %v", got, want)
	}
}

func TestNewRequestClient_AlignsResponseHeaderTimeout(t *testing.T) {
	client := NewRequestClient(10 * time.Minute)
	transport := client.Transport.(*http.Transport)
	if got, want := transport.ResponseHeaderTimeout, 10*time.Minute; got != want {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", got, want)
	}
}

func TestNewStreamingClient_DisablesTotalTimeout(t *testing.T) {
	client := NewStreamingClient(10 * time.Minute)
	if client.Timeout != 0 {
		t.Fatalf("Timeout = %v, want 0 for streaming", client.Timeout)
	}
}

func TestNewStreamingClient_UsesProviderHeaderTimeout(t *testing.T) {
	client := NewStreamingClient(30 * time.Minute)
	transport := client.Transport.(*http.Transport)
	if got, want := transport.ResponseHeaderTimeout, 30*time.Minute; got != want {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", got, want)
	}
}

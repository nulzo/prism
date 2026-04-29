package qwen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSpeechDownloadsReturnedAudioURL(t *testing.T) {
	audioServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/audio.wav", r.URL.Path)
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("wav-bytes"))
	}))
	defer audioServer.Close()

	var got map[string]any
	ttsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/services/aigc/multimodal-generation/generation", r.URL.Path)
		assert.Equal(t, "Bearer dashscope-key", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"request_id": "req-123",
			"output": {
				"finish_reason": "stop",
				"audio": {"url": "` + audioServer.URL + `/audio.wav"}
			}
		}`))
	}))
	defer ttsServer.Close()

	provider, err := NewAdapter(config.ProviderConfig{
		ID:      "qwen",
		Type:    "qwen",
		APIKey:  "dashscope-key",
		BaseURL: "http://chat.invalid/compatible-mode/v1",
		Config: map[string]string{
			"tts_base_url": ttsServer.URL,
		},
	})
	require.NoError(t, err)

	speechProvider, ok := provider.(interface {
		CreateSpeech(context.Context, *api.UpstreamSpeechRequest) (*api.SpeechResponse, error)
	})
	require.True(t, ok)

	resp, err := speechProvider.CreateSpeech(context.Background(), &api.UpstreamSpeechRequest{
		Model: "qwen3-tts-flash",
		Input: "Build something people love.",
		Voice: "Cherry",
		Provider: map[string]any{
			"language_type": "English",
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "audio/wav", resp.ContentType)
	assert.Equal(t, []byte("wav-bytes"), resp.Data)
	assert.Equal(t, "qwen3-tts-flash", got["model"])
	input := got["input"].(map[string]any)
	assert.Equal(t, "Build something people love.", input["text"])
	assert.Equal(t, "Cherry", input["voice"])
	assert.Equal(t, "English", input["language_type"])
}

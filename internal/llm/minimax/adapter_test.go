package minimax

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

func TestCreateSpeechPostsT2ARequestAndDecodesHexAudio(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/t2a_v2", r.URL.Path)
		assert.Equal(t, "Bearer minimax-key", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {"audio": "68656c6c6f", "status": 2},
			"extra_info": {"audio_format": "mp3"},
			"trace_id": "trace-123",
			"base_resp": {"status_code": 0, "status_msg": "success"}
		}`))
	}))
	defer server.Close()

	provider, err := NewAdapter(config.ProviderConfig{
		ID:      "minimax",
		Type:    "minimax",
		APIKey:  "minimax-key",
		BaseURL: server.URL + "/v1",
	})
	require.NoError(t, err)

	speechProvider, ok := provider.(interface {
		CreateSpeech(context.Context, *api.UpstreamSpeechRequest) (*api.SpeechResponse, error)
	})
	require.True(t, ok)

	speed := 1.25
	resp, err := speechProvider.CreateSpeech(context.Background(), &api.UpstreamSpeechRequest{
		Model:          "speech-2.8-hd",
		Input:          "Hello",
		Voice:          "English_expressive_narrator",
		ResponseFormat: "mp3",
		Speed:          &speed,
	})
	require.NoError(t, err)

	assert.Equal(t, "audio/mpeg", resp.ContentType)
	assert.Equal(t, []byte("hello"), resp.Data)
	assert.Equal(t, "speech-2.8-hd", got["model"])
	assert.Equal(t, "Hello", got["text"])
	assert.Equal(t, false, got["stream"])
	assert.Equal(t, "hex", got["output_format"])
	voice := got["voice_setting"].(map[string]any)
	assert.Equal(t, "English_expressive_narrator", voice["voice_id"])
	assert.Equal(t, 1.25, voice["speed"])
	audio := got["audio_setting"].(map[string]any)
	assert.Equal(t, "mp3", audio["format"])
}

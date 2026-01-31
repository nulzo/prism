package api

import "time"

// GenerationResponse mimics the OpenRouter generation response.
type GenerationResponse struct {
	Data GenerationData `json:"data"`
}

type GenerationData struct {
	ID                    string                 `json:"id"`
	UpstreamID            string                 `json:"upstream_id,omitempty"`
	TotalCost             float64                `json:"total_cost"`
	CacheDiscount         *float64               `json:"cache_discount"`
	UpstreamInferenceCost *float64               `json:"upstream_inference_cost"`
	CreatedAt             time.Time              `json:"created_at"`
	Model                 string                 `json:"model"`
	AppID                 string                 `json:"app_id,omitempty"` // Mapped from AppName
	Streamed              bool                   `json:"streamed"`
	Cancelled             bool                   `json:"cancelled"` // Mapped from status code/finish reason
	ProviderName          string                 `json:"provider_name"`
	Latency               float64                `json:"latency"`            // ms
	ModerationLatency     *float64               `json:"moderation_latency"` // null
	GenerationTime        float64                `json:"generation_time"`    // ms
	FinishReason          string                 `json:"finish_reason"`
	TokensPrompt          int                    `json:"tokens_prompt"`
	TokensCompletion      int                    `json:"tokens_completion"`
	NativeTokensPrompt    int                    `json:"native_tokens_prompt"`
	NativeTokensCompletion int                   `json:"native_tokens_completion"`
	NativeTokensReasoning *int                   `json:"native_tokens_reasoning"`
	NativeTokensCached    *int                   `json:"native_tokens_cached"`
	NumMediaPrompt        *int                   `json:"num_media_prompt"`
	NumInputAudioPrompt   *int                   `json:"num_input_audio_prompt"`
	NumSearchResults      *int                   `json:"num_search_results"`
	Usage                 float64                `json:"usage"` // USD
	IsBYOK                bool                   `json:"is_byok"`
	NativeFinishReason    string                 `json:"native_finish_reason"`
	APIType               string                 `json:"api_type"` // "chat"
	Router                string                 `json:"router"`   // "model-router"
}

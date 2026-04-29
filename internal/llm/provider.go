package llm

import (
	"context"

	"github.com/nulzo/model-router-api/pkg/api"
)

type ProviderName string

const (
	Ollama      ProviderName = "ollama"
	OpenAI      ProviderName = "openai"
	Anthropic   ProviderName = "anthropic"
	Google      ProviderName = "google"
	Moonshot    ProviderName = "moonshot"
	ElevenLabs  ProviderName = "elevenlabs"
	CosyVoice   ProviderName = "cosyvoice"
	Qwen        ProviderName = "qwen"
	Qwen3       ProviderName = "qwen3"
	DeepSeek    ProviderName = "deepseek"
	Zai         ProviderName = "zai"
	MiniMax     ProviderName = "minimax"
	HuggingFace ProviderName = "huggingface"
)

type Provider interface {
	Name() string
	Type() string // e.g., "openai", "anthropic"
	Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error)
	Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error)
	Health(ctx context.Context) error
}

type SpeechProvider interface {
	CreateSpeech(ctx context.Context, req *api.UpstreamSpeechRequest) (*api.SpeechResponse, error)
	StreamSpeech(ctx context.Context, req *api.UpstreamSpeechRequest, write api.SpeechStreamWriter) error
}

type ToolCallingMode string

const (
	ToolCallingUnsupported  ToolCallingMode = "unsupported"
	ToolCallingOpenAICompat ToolCallingMode = "openai_compatible"
	ToolCallingNative       ToolCallingMode = "native"
)

type Capabilities struct {
	ToolCalling ToolCallingMode
}

type CapabilityDescriber interface {
	Capabilities() Capabilities
}

func DescribeCapabilities(p Provider) Capabilities {
	if describer, ok := p.(CapabilityDescriber); ok {
		return describer.Capabilities()
	}
	return Capabilities{ToolCalling: ToolCallingUnsupported}
}

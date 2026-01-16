package providers

type Provider string

const (
	Ollama    Provider = "ollama"
	OpenAI    Provider = "openai"
	Anthropic Provider = "anthropic"
	Google    Provider = "google"
)

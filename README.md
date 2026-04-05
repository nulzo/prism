<div align="center">
  <a href="https://github.com/nulzo/prism">
    <img src="docs/prismv2.png" width="240" alt="prism">
  </a>
</div>

# Prism

Ultra-fast, low-latency, self-hostable AI gateway and model router.

Prism acts as a unified API gateway that allows you to interact with multiple AI providers (OpenAI, Anthropic, Google, etc...) using a single, standard API format.

## Features

- **Unified API**: Call any supported LLM or TTS model using standard OpenAI-compatible endpoints (`/v1/chat/completions`).
- **Text Generation**: Support for OpenAI, Anthropic, Google Gemini, Moonshot, and local models via Ollama.
- **Image Generation**: Support for Black Forest Labs (BFL) and others via standard chat completion structures.
- **Text-to-Speech (TTS)**: Generate audio from text using ElevenLabs, CosyVoice, or Qwen3-TTS.
- **High Performance**: Built in Go for minimal latency and high concurrency.
- **Caching**: Built-in support for Redis or in-memory caching to reduce latency and API costs.
- **Rate Limiting**: Configurable request limits and burst capacities.
- **Analytics & Telemetry**: Built-in database for tracking usage, costs, and request metrics.

## Installation

todo... using with docker

## Supported Providers

Prism currently supports the following providers:

### LLM Providers
- `openai` (OpenAI)
- `anthropic` (Anthropic)
- `google` (Google Gemini)
- `moonshot` (Moonshot AI)
- `ollama` (Local models via Ollama)

### Image Generation
- `bfl` (Black Forest Labs)

### Text-to-Speech (TTS)
- `elevenlabs` (ElevenLabs)
- `cosyvoice` (CosyVoice - Local)
- `qwen3` (Qwen3-TTS - Local via vLLM-Omni)

## Configuration

Prism is configured via a `config.yaml` file (or environment variables). Here is an example configuration enabling multiple providers:

```yaml
server:
  port: 8080
  env: development
  auth_enabled: false
  api_keys: ["your-secure-api-key"]

rate_limit:
  requests_per_second: 100
  burst: 200

database:
  path: "router.db"

redis:
  enabled: false
  addr: "localhost:6379"

providers:
  - id: openai
    type: openai
    name: OpenAI
    api_key: "sk-..."
    enabled: true
    requires_auth: true

  - id: anthropic
    type: anthropic
    name: Anthropic
    api_key: "sk-ant-..."
    enabled: true

  - id: ollama
    type: ollama
    name: Ollama
    base_url: "http://localhost:11434"
    enabled: true

  - id: elevenlabs
    type: elevenlabs
    name: ElevenLabs
    api_key: "your-elevenlabs-key"
    enabled: true

  - id: cosyvoice
    type: cosyvoice
    name: CosyVoice
    base_url: "http://localhost:50000" # Default CosyVoice FastAPI port
    enabled: true

  - id: qwen3
    type: qwen3
    name: Qwen3-TTS
    base_url: "http://localhost:8000/v1" # Default vLLM-Omni port
    enabled: true
```

## Usage

### Chat Completions (LLMs)

Send a standard chat completion request to Prism, specifying the provider and model in the format `<provider_id>/<model_name>`:

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-api-key>" \
  -d '{
    "model": "openai/gpt-4o",
    "messages": [
      {
        "role": "user",
        "content": "Hello, how are you?"
      }
    ]
  }'
```

### Text-to-Speech (TTS)

To generate audio, send a standard chat completion request with the `modalities` and `audio` fields configured. The response will contain a base64-encoded audio string in the `audio.data` field.

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-api-key>" \
  -d '{
    "model": "elevenlabs/JBFqnCBsd6RMkjVDRZzb",
    "messages": [
      {
        "role": "user",
        "content": "Hello! This is a test of the text-to-speech system."
      }
    ],
    "modalities": ["text", "audio"],
    "audio": {
      "format": "mp3_44100_128"
    }
  }'
```
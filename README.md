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

## Plugins

Plugins are a deterministic operation applied to a request. When requested, plugins will ALWAYS be applied to a specific operation.

Consider the request:

```json
{
  "model": "google/gemini-3.1-flash-lite-preview",
  "messages": [
    {
      "role": "user",
      "content": "Why is the sky blue?"
    }
  ],
  "plugins": [
    {
      "id": "context-compression",
      "enabled": true
    }
  ]
}
```

The plugins array (an array of plugin identifier objects) contains the plugins that are requested to be applied to the operation. In this example, it's the context compression plugin. When the plugin is requested to be used, it will essentially function as a middleware that intercepts the request and applies the plugin _before_ a request is made to the underlying model.

As stated, when a plugin is requested, it will _always_ be applied to the underlying operation. Functionally, from a request pipelining perspective, this operation is applied through ingestion middleware, similar to how something like auth might look (ie it is applied before downstream requests are made).

### Context Compression

The context compression plugin is used to force the LLM context (previous history/system) to be compressed in order to save on costs. It drastically compresses the context, while trying not to lose any valuable information held within the context (as close to lossless compression as is possible with prompting, lol).

### Custom Plugins

One of the main ideas with plugins/extensions was to make them easy to create, easy to extend, and allow users to create their own. Because of this, it is relatively straightforward to create both custom extensions and custom plugins. 

For demonstrative purposes, we can make a really simple plugin. This plugin will do nothing more than set the temperature of the request. Obviously, there are better ways to do _this_ specific use case, but it's the most simple idea i can think of right now. To create the plugin, create a file within the plugin dir, and call it `<plugin_name>.go`. The contents could be:

```go
import (
	"context"
)

type TemperaturePlugin struct {
	Temperature int
}

func NewTemperaturePlugin(temperature int) *TemperaturePlugin {
	return &TemperaturePlugin{Temperature: temperature}
}

func (p *TemperaturePlugin) ID() string {
	return "temperature"
}

func (p *TemperaturePlugin) PreProcess(ctx context.Context, pCtx *Context) error {

	pCtx.Request. = compressed
	return nil
}

func (p *TemperaturePlugin) PostProcess(ctx context.Context, pCtx *Context) error {
	// no post processing needed for this usecase
	return nil
}
```

Once you register the plugin, you can then make a request like:

```json
{
  "model": "google/gemini-3.1-flash-lite-preview",
  "messages": [
    {
      "role": "user",
      "content": "Hey!"
    }
  ],
  "plugins": [
    {
      "id": "temperature",
      "enabled": true,
      "config": {
        "temperature": 2,
      }
    }
  ]
}
```

which should send the underlying request with the temperature set to 2.

## Extensions

Extensions are similar to plugins, but are _nondeterministic_. That is, when they are requested to be used, there is **no** gurantee that the extension will be used. 

Conceptually, they can be thought of as similar to tool calling. An extension can be added to request, and if the underlying model deems it relevant to be used, it will be used. Consider the following:

```json
{
  "model": "google/gemini-3.1-flash-lite-preview",
  "messages": [
    {
      "role": "user",
      "content": "What are the top 3 AI headlines today?"
    }
  ],
  "extensions": [
    {
      "id": "prism:web_search",
      "enabled": true,
      "config": {
        "max_results": 3,
        "searxng_url": "http://localhost:8888"
      }
    },
    {
      "id": "prism:datetime",
      "enabled": true,
      "config": {
        "default_timezone": "Asia/Tokyo"
      }
    }
  ]
}
```

This request is decalring: "I want to provide the model with the _option_ to use these extensions _IFF_ the extensions are relevant to the underlying request".

In practice, this should reduce wasted usage, reduce unnecessary calls, etc. As an example, if the above request contained the question: `"who makes the iPhone?"`, it would (hopefully) _not_ call either of the two extensions provided.

### Web Search

The web search extension grounds requests with search results that are _agnostic_ to the underlying model/provider. That is, it does not use any web tools provided by the provider (like how google models provide web search grounding options, openai allows you to let the models search the web, etc etc.). Because of this, we can apply web search grounding to _any_ model (even ollama ones) without relying on any one providers underlying methods.

Behind the scenes, this extension does require a bit of overhead that isn't provided through the general runtime of prism. The tool expects `searXNG` to be running as a disposable container on the runtime server. This is configrable through prism's config, and we are actively looking at ways of allowing users to have a more seamless experience when standing up prism gateways. 

For now, to configure searXNG on a system, a user can (at least the way I configure it):

Create a directory to house searxng config on your system:
`mkdir ~/.searxng`

Create directories to mount to the docker runtime:
```
mkdir ~/.searxng/data
mkdir ~/.searxng/config
```

Next, you'll need to create a YAML file for searxng to run as a standalone API (otherwise it will 403 when you try to call the docker runtime). Create the file at `~/.searxng/config/settings.yml`, and add the following content:

```
use_default_settings: true

server:
  secret_key: "replace-this-with-a-random-secret"
  limiter: false

search:
  formats:
    - html
    - json
```

Once you've done all of that, you can startup a docker runtime accessible on your systems port `8888` and mount the volumes to those files by running:

```docker run --name searxng -d \
  -p 8888:8080 \
  -v "$(pwd)/config:/etc/searxng" \
  -v "$(pwd)/data:/var/cache/searxng" \
  docker.io/searxng/searxng:latest
```

This will allow you to get a searxng runtime stood up. You can test the connection by running a simple query like: `curl -i "http://localhost:8888/search?q=howdy&format=json"`

If you get a resonse like:
```
HTTP/1.1 403 Forbidden
content-type: text/html; charset=utf-8
content-length: 213
server-timing: total;dur=6.267, render;dur=0
x-content-type-options: nosniff
x-download-options: noopen
x-robots-tag: noindex, nofollow
referrer-policy: no-referrer
server: granian
```
Then you probably skipped adding the settings.yaml file shown above, or mismounted it. If it's still not working, then the underlying searxng config requirements might've changed, so read through their docs and double check.


### Datetime

The datetime extension will inject the current datetime of the client (configurable through the request) in order to give accurate date context to the model/request.

For example, the following:
```json
{
  "model": "google/gemini-3.1-flash-lite-preview",
  "messages": [
    {
      "role": "user",
      "content": "What day is it today?"
    }
  ],
  "extensions": [
    {
      "id": "prism:datetime",
      "enabled": true,
      "config": {
        "default_timezone": "Asia/Tokyo"
      }
    }
  ]
}
```

This request, since it includes the datetime extension request, will first check if the query should be fortified with date information, and, if it determines that it should be, it will inject the datetime into the query. This can be useful when a user asks something like `"it's really windy in Chicago today. Is that normal for this time of the year?"`. This query will certainly result in the datetime extension triggering, and it will inject the date into the users question so that the model has context of what "today" actually means.

### Custom extensions

One of the main ideas with plugins/extensions was to make them easy to create, easy to extend, and allow users to create their own. Because of this, it is relatively straightforward to create both custom extensions and custom plugins. 

To create a custom extension, you need to add a new file (generally `<extension_name>.go`) within the extensions directory at `internal/extension/*`. Since the underlying request flow uses a pipeline design pattern, you should just be able to write the custom Go code, and then just register the extension within the gateway pipeline.

For example sake, let's create a simple extension for injecting the current Go version. The custom extension should use the extension interface, and the implementation could look like:

```go
type GolangVersionExtension struct{}

func NewGolangVersionExtension() *GolangVersionExtension {
	return &GolangVersionExtension{}
}

func (t *GolangVersionExtension) Name() string {
	return "prism:golang_version"
}

func (t *GolangVersionExtension) BuildTool(config api.ExtensionConfig) (api.Tool, error) {
	return api.Tool{
		Type: "function",
		Function: api.FunctionDescription{
			Name:        t.Name(),
			Description: "Get the runtimes golang version."
		},
	}, nil
}

func (t *GolangVersionExtension) Execute(ctx context.Context, config api.ExtensionConfig, args []byte) (string, error) {
	v := GoVersion()
	result := map[string]string{
		"version": v
	}

	resBytes, _ := json.Marshal(result)
	return string(resBytes), nil
}
```

which can then be registered in the pipeline by adding:

```go
eReg.Register(extension.NewGolangVersionExtension())
```

Then, as simple as that, you can make a request like:

```json
{
  "model": "google/gemini-3.1-flash-lite-preview",
  "messages": [
    {
      "role": "user",
      "content": "Which Golang version am i running? you should make yourself sound like a pirate"
    }
  ],
  "extensions": [
    {
      "id": "prism:golang_version",
      "enabled": true
    }
  ]
}
```

which should yield your go runtime version (as well as some response flavor)!

Eventually, it would be fun to get an external registry of extensions for user supported extensions, but this is dangerous for obvious reasons. If you have any ideas of how it could be done, please let me know :)

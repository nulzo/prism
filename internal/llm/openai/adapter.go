package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/httpclient"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/llm/processing"
	"github.com/nulzo/model-router-api/pkg/api"
)

func init() {
	llm.Register("openai", NewAdapter)
}

type Adapter struct {
	config config.ProviderConfig
	client *http.Client
	stream *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
	fmt.Printf("DEBUG: OpenAI Adapter Init. ID=%s BaseURL='%s' APIKeyLen=%d\n", config.ID, config.BaseURL, len(config.APIKey))
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}

	timeout := 10 * time.Minute
	if config.Timeout != "" {
		if d, err := time.ParseDuration(config.Timeout); err == nil {
			timeout = d
		} else {
			fmt.Printf("Warning: Invalid timeout format for provider %s: %v. Using default %v.\n", config.ID, err, timeout)
		}
	}

	return &Adapter{
		config: config,
		client: httpclient.NewRequestClient(timeout),
		stream: httpclient.NewStreamingClient(timeout),
	}, nil
}

func (a *Adapter) Name() string {
	return a.config.ID
}

func (a *Adapter) Type() string {
	return "openai"
}

// upstreamErrorResponse mirrors the standard OpenAI error shape
type upstreamErrorResponse struct {
	Error struct {
		Message string      `json:"message"`
		Type    string      `json:"type"`
		Param   interface{} `json:"param"`
		Code    interface{} `json:"code"`
	} `json:"error"`
}

type imageGenerationRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type imageGenerationResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		URL           string `json:"url,omitempty"`
		B64JSON       string `json:"b64_json,omitempty"`
		RevisedPrompt string `json:"revised_prompt,omitempty"`
	} `json:"data"`
	Usage *api.ResponseUsage `json:"usage,omitempty"`
}

type speechGenerationRequest struct {
	Model          string   `json:"model"`
	Input          string   `json:"input"`
	Voice          string   `json:"voice"`
	ResponseFormat string   `json:"response_format,omitempty"`
	Speed          *float64 `json:"speed,omitempty"`
}

func (a *Adapter) handleUpstreamError(err error) error {
	var upstreamErr *httpclient.UpstreamError
	if !errors.As(err, &upstreamErr) {
		return err
	}

	// parse the specific upstream error format
	var apiErr upstreamErrorResponse
	if jsonErr := json.Unmarshal(upstreamErr.Body, &apiErr); jsonErr != nil {
		// if we can't parse it, return a generic upstream error
		return api.NewError(
			upstreamErr.StatusCode,
			"Upstream Error",
			string(upstreamErr.Body),
			api.WithLog(err),
		)
	}

	// create a nice RFC 9457 problem
	return api.NewError(
		upstreamErr.StatusCode,
		"Upstream Provider Error",
		apiErr.Error.Message,
		api.WithType("about:blank"),
		api.WithExtension("upstream_code", apiErr.Error.Code),
		api.WithExtension("upstream_type", apiErr.Error.Type),
		api.WithExtension("upstream_param", apiErr.Error.Param),
		api.WithLog(err),
	)
}

func (a *Adapter) Capabilities() llm.Capabilities {
	return llm.Capabilities{ToolCalling: llm.ToolCallingOpenAICompat}
}

func (a *Adapter) CreateSpeech(ctx context.Context, req *api.UpstreamSpeechRequest) (*api.SpeechResponse, error) {
	var audio bytes.Buffer
	contentType := ""
	if err := a.StreamSpeech(ctx, req, func(nextContentType string, chunk []byte) error {
		if contentType == "" {
			contentType = nextContentType
		}
		_, err := audio.Write(chunk)
		return err
	}); err != nil {
		return nil, err
	}
	return &api.SpeechResponse{
		Data:        audio.Bytes(),
		ContentType: contentType,
	}, nil
}

func (a *Adapter) StreamSpeech(ctx context.Context, req *api.UpstreamSpeechRequest, write api.SpeechStreamWriter) error {
	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
		"Content-Type":  "application/json",
	}
	if org, ok := a.config.Config["organization"]; ok {
		headers["OpenAI-Organization"] = org
	}

	payload := speechGenerationRequest{
		Model:          req.Model,
		Input:          req.Input,
		Voice:          req.Voice,
		ResponseFormat: req.ResponseFormat,
		Speed:          req.Speed,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/audio/speech", strings.TrimRight(a.config.BaseURL, "/"))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return a.handleUpstreamError(&httpclient.UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       respBody,
			URL:        url,
		})
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = speechContentType(req.ResponseFormat)
	}
	buffer := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			if err := write(contentType, buffer[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func (a *Adapter) generateImage(ctx context.Context, req *api.UpstreamChatRequest, headers map[string]string) (*api.ChatResponse, error) {
	prompt := imagePrompt(req.Messages)
	if prompt == "" {
		return nil, api.BadRequestError("image generation requires a non-empty text prompt")
	}
	references := imageReferences(req.Messages)
	if len(references) > 0 {
		return a.editImage(ctx, req, headers, prompt, references)
	}

	var imgResp imageGenerationResponse
	url := fmt.Sprintf("%s/images/generations", strings.TrimRight(a.config.BaseURL, "/"))
	payload := imageGenerationRequest{
		Model:  req.Model,
		Prompt: prompt,
	}
	if err := httpclient.SendRequest(ctx, a.client, "POST", url, headers, payload, &imgResp); err != nil {
		return nil, a.handleUpstreamError(err)
	}

	return imageGenerationToChatResponse(req.Model, prompt, &imgResp)
}

func (a *Adapter) editImage(ctx context.Context, req *api.UpstreamChatRequest, headers map[string]string, prompt string, references []string) (*api.ChatResponse, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", req.Model); err != nil {
		return nil, err
	}
	if err := writer.WriteField("prompt", prompt); err != nil {
		return nil, err
	}

	for i, ref := range references {
		if err := appendImageReference(writer, i, ref); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	var imgResp imageGenerationResponse
	url := fmt.Sprintf("%s/images/edits", strings.TrimRight(a.config.BaseURL, "/"))
	if err := a.sendMultipart(ctx, url, headers, writer.FormDataContentType(), &body, &imgResp); err != nil {
		return nil, a.handleUpstreamError(err)
	}

	return imageGenerationToChatResponse(req.Model, prompt, &imgResp)
}

func (a *Adapter) sendMultipart(ctx context.Context, url string, headers map[string]string, contentType string, body io.Reader, response any) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentType)
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return &httpclient.UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       respBody,
			URL:        url,
		}
	}

	if response != nil {
		if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}
	return nil
}

func (a *Adapter) Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	var resp api.ChatResponse
	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}

	if org, ok := a.config.Config["organization"]; ok {
		headers["OpenAI-Organization"] = org
	}

	if isImageGenerationModel(req.Model) {
		return a.generateImage(ctx, req, headers)
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))

	req.Stream = false

	payload := a.buildUpstreamPayload(req)

	if err := httpclient.SendRequest(ctx, a.client, "POST", url, headers, payload, &resp); err != nil {
		return nil, a.handleUpstreamError(err)
	}

	// OpenAI-compat providers return reasoning in a few shapes:
	//   * DeepSeek, Qwen, Moonshot: `message.reasoning_content` (aliased
	//     into Reasoning by ChatMessage.UnmarshalJSON already).
	//   * OpenRouter, xAI: `message.reasoning`.
	//   * Legacy/non-compliant: `<think>...</think>` tags inside content.
	// We only fall back to the tag-extraction heuristic when the upstream
	// didn't populate a dedicated field, so a reasoning-aware provider
	// doesn't pay for redundant post-processing.
	for i := range resp.Choices {
		choice := &resp.Choices[i]
		if choice.Message == nil {
			continue
		}
		if choice.Message.Reasoning != "" {
			continue
		}
		content, reasoning := processing.ExtractThinking(choice.Message.Content.Text)
		if reasoning != "" {
			choice.Message.Content.Text = content
			choice.Message.Reasoning = reasoning
		}
	}

	return &resp, nil
}

func (a *Adapter) Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	ch := make(chan api.StreamResult)

	if isImageGenerationModel(req.Model) {
		go func() {
			defer close(ch)
			resp, err := a.Chat(ctx, req)
			if err != nil {
				ch <- api.StreamResult{Err: err}
				return
			}
			ch <- api.StreamResult{Response: imageChatResponseToChunk(resp)}
		}()
		return ch, nil
	}

	req.Stream = true
	req.StreamOptions = &api.StreamOptions{IncludeUsage: true}
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))

	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}
	if org, ok := a.config.Config["organization"]; ok {
		headers["OpenAI-Organization"] = org
	}

	payload := a.buildUpstreamPayload(req)

	go func() {
		defer close(ch)

		// Per-choice parsers that handle the legacy `<think>` tag case. We
		// only route content through the parser when the provider hasn't
		// already populated a first-class reasoning field for the same
		// chunk; this keeps the fast path (modern providers) free of
		// any extra string work.
		parsers := make(map[int]*processing.StreamParser)

		err := httpclient.StreamRequest(ctx, a.stream, "POST", url, headers, payload, func(line string) error {
			if !strings.HasPrefix(line, "data: ") {
				return nil
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return nil
			}

			var chatResp api.ChatResponse
			if err := json.Unmarshal([]byte(data), &chatResp); err != nil {
				return nil
			}

			for i := range chatResp.Choices {
				choice := &chatResp.Choices[i]
				if choice.Delta == nil {
					continue
				}

				// If the provider sent a native reasoning delta (either
				// `reasoning` or `reasoning_content`, aliased by the
				// ChatMessage unmarshaller), skip the tag-scanner entirely.
				if choice.Delta.Reasoning != "" {
					continue
				}

				idx := choice.Index
				parser, ok := parsers[idx]
				if !ok {
					parser = processing.NewStreamParser()
					parsers[idx] = parser
				}

				c, r := parser.Process(choice.Delta.Content.Text)
				choice.Delta.Content.Text = c
				if r != "" {
					choice.Delta.Reasoning = r
				}
			}

			ch <- api.StreamResult{Response: &chatResp}
			return nil
		})
		if err != nil {
			ch <- api.StreamResult{Err: a.handleUpstreamError(err)}
		}
	}()

	return ch, nil
}

func isImageGenerationModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "gpt-image-") ||
		strings.HasPrefix(model, "dall-e-") ||
		model == "chatgpt-image-latest"
}

func imagePrompt(messages []api.ChatMessage) string {
	var parts []string
	for _, msg := range messages {
		if msg.Content.Text != "" {
			parts = append(parts, msg.Content.Text)
		}
		for _, part := range msg.Content.Parts {
			if part.Type == "text" && part.Text != "" {
				parts = append(parts, part.Text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func imageReferences(messages []api.ChatMessage) []string {
	var refs []string
	for _, msg := range messages {
		for _, part := range msg.Content.Parts {
			if part.Type == "image_url" && part.ImageURL != nil && part.ImageURL.URL != "" {
				refs = append(refs, part.ImageURL.URL)
			}
		}
		for _, image := range msg.Images {
			if image.ImageURL != nil && image.ImageURL.URL != "" {
				refs = append(refs, image.ImageURL.URL)
			}
		}
	}
	return refs
}

func appendImageReference(writer *multipart.Writer, index int, ref string) error {
	image, err := processing.ProcessImageURL(ref)
	if err != nil {
		return api.BadRequestError(fmt.Sprintf("invalid image reference %d: %v", index+1, err))
	}
	data, err := base64.StdEncoding.DecodeString(image.Data)
	if err != nil {
		return api.BadRequestError(fmt.Sprintf("invalid image reference %d: %v", index+1, err))
	}

	mediaType := image.MediaType
	if parsed, _, err := mime.ParseMediaType(mediaType); err == nil && parsed != "" {
		mediaType = parsed
	}
	if mediaType == "" {
		mediaType = "image/png"
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image[]"; filename="reference-%d%s"`, index+1, imageExtension(mediaType)))
	header.Set("Content-Type", mediaType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(data)
	return err
}

func imageExtension(mediaType string) string {
	extensions, err := mime.ExtensionsByType(mediaType)
	if err == nil && len(extensions) > 0 {
		return extensions[0]
	}
	return ".png"
}

func imageGenerationToChatResponse(model string, prompt string, imgResp *imageGenerationResponse) (*api.ChatResponse, error) {
	images := make([]api.ContentPart, 0, len(imgResp.Data))
	caption := prompt
	for _, item := range imgResp.Data {
		if item.RevisedPrompt != "" {
			caption = item.RevisedPrompt
		}
		switch {
		case item.B64JSON != "":
			images = append(images, api.ContentPart{
				Type:     "image_url",
				ImageURL: &api.ImageURL{URL: "data:image/png;base64," + item.B64JSON},
			})
		case item.URL != "":
			images = append(images, api.ContentPart{
				Type:     "image_url",
				ImageURL: &api.ImageURL{URL: item.URL},
			})
		}
	}
	if len(images) == 0 {
		return nil, api.ProviderError("OpenAI image generation returned no images", nil)
	}

	created := imgResp.Created
	if created == 0 {
		created = time.Now().Unix()
	}

	return &api.ChatResponse{
		ID:      fmt.Sprintf("img-%d", created),
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []api.Choice{{
			Index: 0,
			Message: &api.ChatMessage{
				Role:    "assistant",
				Content: api.Content{Text: caption},
				Images:  images,
			},
			FinishReason: "stop",
		}},
		Usage: imgResp.Usage,
	}, nil
}

func imageChatResponseToChunk(resp *api.ChatResponse) *api.ChatResponse {
	if resp == nil {
		return nil
	}
	chunk := *resp
	chunk.Object = "chat.completion.chunk"
	for i := range chunk.Choices {
		choice := &chunk.Choices[i]
		if choice.Message == nil {
			continue
		}
		delta := *choice.Message
		choice.Delta = &delta
		choice.Message = nil
	}
	return &chunk
}

// upstreamPayload mirrors the subset of UpstreamChatRequest that
// OpenAI-compatible providers accept plus the native reasoning control:
// `reasoning_effort` for OpenAI-style upstreams, or the structured
// `reasoning` object for OpenRouter-style gateways. We purposely do NOT forward the generic
// `reasoning` field by default because strict OpenAI-compatible shims
// (Gemini, DeepSeek direct, Moonshot) will 400 on unknown fields.
type upstreamPayload struct {
	*api.UpstreamChatRequest
	Messages        []processing.CompatMessage `json:"messages"`
	ReasoningEffort string                     `json:"reasoning_effort,omitempty"`
	Reasoning       *upstreamReasoning         `json:"reasoning,omitempty"`
}

type upstreamReasoning struct {
	Effort    string `json:"effort,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Exclude   bool   `json:"exclude,omitempty"`
}

// buildUpstreamPayload translates the router's normalized ReasoningConfig
// into the native shape each OpenAI-compatible upstream actually accepts.
// It never mutates the caller's request. When reasoning is not requested
// the original request is returned verbatim so the JSON encoder takes
// the zero-copy fast path.
func (a *Adapter) buildUpstreamPayload(req *api.UpstreamChatRequest) any {
	if req == nil {
		return req
	}
	r := req.Reasoning
	// Always strip the router-only Reasoning field before serialization;
	// non-OpenRouter upstreams reject unknown fields and we re-add the
	// native equivalents below when a reasoning request is actually active.
	inner := *req
	inner.Reasoning = nil

	out := upstreamPayload{
		UpstreamChatRequest: &inner,
		Messages:            processing.FormatOpenAIMessages(inner.Messages),
	}

	if r == nil || (!r.IsEnabled() && !r.Exclude) {
		return out
	}

	// Pass the full object through only to upstreams that we know accept
	// it (OpenRouter and other gateway-style endpoints). This check is
	// deliberately loose — exact hostname matching is brittle across self-
	// hosted OpenRouter deployments.
	if strings.Contains(a.config.BaseURL, "openrouter") {
		out.Reasoning = &upstreamReasoning{
			Effort:    r.Effort,
			MaxTokens: r.MaxTokens,
			Exclude:   r.Exclude,
		}
	} else if r.Effort != "" {
		out.ReasoningEffort = normalizeEffort(r.Effort)
	}

	return out
}

// normalizeEffort clamps OpenRouter-style effort values to the subset OpenAI
// actually accepts. `xhigh` / `none` are OpenRouter extensions; we map them
// to the nearest valid bucket rather than drop the field.
func normalizeEffort(e string) string {
	switch strings.ToLower(strings.TrimSpace(e)) {
	case "xhigh", "high":
		return "high"
	case "medium":
		return "medium"
	case "low":
		return "low"
	case "minimal":
		return "minimal"
	case "none":
		// OpenAI has no explicit off-switch; omit the header by returning "".
		return ""
	default:
		return "medium"
	}
}

func speechContentType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "flac":
		return "audio/flac"
	case "opus":
		return "audio/ogg"
	case "aac":
		return "audio/aac"
	case "pcm", "pcm16":
		return "audio/pcm"
	default:
		return "audio/pcm"
	}
}

func (a *Adapter) Health(ctx context.Context) error {
	url := fmt.Sprintf("%s/models", strings.TrimRight(a.config.BaseURL, "/"))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+a.config.APIKey)

	if org, ok := a.config.Config["organization"]; ok {
		req.Header.Set("OpenAI-Organization", org)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}

	return nil
}

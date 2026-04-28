package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nulzo/model-router-api/internal/analytics"
	"github.com/nulzo/model-router-api/internal/extension"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"github.com/nulzo/model-router-api/internal/plugin"
	"github.com/nulzo/model-router-api/internal/store"
	"github.com/nulzo/model-router-api/internal/store/cache"
	"github.com/nulzo/model-router-api/internal/store/model"
	"github.com/nulzo/model-router-api/pkg/api"
	"go.uber.org/zap"
)

var (
	ErrProviderNotFound = errors.New("provider not found")
	ErrRouteNotFound    = errors.New("no provider configured for this model")
)

// Service defines the business logic for routing requests.
type Service interface {
	// RegisterProvider registers a new model provider.
	RegisterProvider(ctx context.Context, p llm.Provider, models []api.ModelDefinition) error

	GetProviderForModel(ctx context.Context, modelID string) (llm.Provider, string, error)
	ListAllModels(ctx context.Context, filter api.ModelFilter) ([]api.Model, error)
	Chat(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error)
	StreamChat(ctx context.Context, req *api.ChatRequest) (<-chan api.StreamResult, error)
	CreateSpeech(ctx context.Context, req *api.SpeechRequest) (*api.SpeechResponse, error)
	StreamSpeech(ctx context.Context, req *api.SpeechRequest, write api.SpeechStreamWriter) error
}

type service struct {
	logger    *zap.Logger
	repo      store.Repository
	ingestor  analytics.Ingestor
	cache     cache.CacheService
	mu        sync.RWMutex
	providers map[string]llm.Provider
	models    map[string]api.ModelDefinition

	plugins    *plugin.Registry
	extensions *extension.Registry
}

// NewService wires the gateway with static models.
func NewService(logger *zap.Logger, repo store.Repository, ingestor analytics.Ingestor, cache cache.CacheService) Service {
	pReg := plugin.NewRegistry()
	pReg.Register(plugin.NewContextCompressionPlugin(10))

	eReg := extension.NewRegistry()
	eReg.Register(extension.NewDatetimeExtension())
	searxngURL := os.Getenv("SEARXNG_URL")
	if searxngURL == "" {
		searxngURL = "http://localhost:8888"
	}
	eReg.Register(extension.NewWebSearchExtension(searxngURL))

	return &service{
		logger:     logger,
		repo:       repo,
		ingestor:   ingestor,
		cache:      cache,
		providers:  make(map[string]llm.Provider),
		models:     make(map[string]api.ModelDefinition),
		plugins:    pReg,
		extensions: eReg,
	}
}

func (s *service) RegisterProvider(ctx context.Context, p llm.Provider, models []api.ModelDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers[p.Name()] = p
	for _, m := range models {
		s.models[m.ID] = m
	}
	return nil
}

func (s *service) Chat(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {
	provider, upstreamModelID, err := s.GetProviderForModel(ctx, req.Model)
	if err != nil {
		return nil, err
	}

	reqClone := *req
	s.applyModelParameterPolicy(&reqClone, req.Model)
	reqClone.Model = upstreamModelID

	// Prefer the request-scoped generation id (set by the HTTP layer so the
	// `X-Generation-Id` header, the response body, and the analytics record
	// share a value). Fall back to a fresh UUID when invoked outside the HTTP
	// path (tests, future internal callers).
	genID := GenerationID(ctx)
	if genID == "" {
		u, err := uuid.NewRandom()
		if err != nil {
			return nil, fmt.Errorf("failed to generate UUID: %v", err)
		}
		genID = u.String()
	}

	start := time.Now()

	// Create orchestrator
	orchestrator := NewPipelineOrchestrator(s.plugins, s.extensions)
	s.logger.Debug("executing request through pipeline",
		zap.String("provider", provider.Name()),
		zap.String("model", reqClone.Model),
		zap.Int("plugins_requested", len(reqClone.Plugins)),
		zap.Int("extensions_requested", len(reqClone.Extensions)),
	)
	resp, err := orchestrator.Execute(ctx, &reqClone, provider)

	latency := time.Since(start)

	var userID, apiKeyID, appName string
	if val, ok := ctx.Value(store.ContextKeyAppName).(string); ok {
		appName = val
	}
	if apiKey, ok := ctx.Value(store.ContextKeyAPIKey).(*model.APIKey); ok {
		userID = apiKey.UserID
		apiKeyID = apiKey.ID
	} else {
		if appName != "" {
			userID = string(api.Anonymous)
			apiKeyID = string(api.Anonymous)
		} else {
			userID = string(api.System)
			apiKeyID = string(api.System)
		}
	}

	if err != nil {
		statusCode := 500
		finishReason := "error"
		if errors.Is(err, context.Canceled) {
			statusCode = 499
			finishReason = "canceled"
		}

		s.ingestor.Log(&model.RequestLog{
			ID:              genID,
			UserID:          userID,
			APIKeyID:        apiKeyID,
			AppName:         appName,
			ProviderID:      provider.Name(),
			ModelID:         req.Model,
			UpstreamModelID: upstreamModelID,
			FinishReason:    finishReason,
			StatusCode:      statusCode,
			LatencyMS:       latency.Milliseconds(),
			IsStreamed:      false,
			CreatedAt:       time.Now(),
		})
		return nil, fmt.Errorf("provider execution failed: %w", err)
	}

	finishReason := ""
	if len(resp.Choices) > 0 {
		finishReason = resp.Choices[0].FinishReason
	}

	log := &model.RequestLog{
		ID:               genID,
		UserID:           userID,
		APIKeyID:         apiKeyID,
		AppName:          appName,
		ProviderID:       provider.Name(),
		ModelID:          req.Model,
		UpstreamModelID:  upstreamModelID,
		UpstreamRemoteID: resp.ID,
		FinishReason:     finishReason,
		StatusCode:       200,
		LatencyMS:        latency.Milliseconds(),
		IsStreamed:       false,
		CreatedAt:        time.Now(),
	}

	resp.ID = genID

	if resp.Usage != nil {
		log.InputTokens = resp.Usage.PromptTokens
		log.OutputTokens = resp.Usage.CompletionTokens
		log.CachedTokens = 0 // Will be updated from details

		details := &model.UsageDetails{
			WebSearchRequests: 0, // Default
		}

		if resp.Usage.PromptTokensDetails != nil {
			details.PromptTokensCached = resp.Usage.PromptTokensDetails.CachedTokens
			details.PromptTokensCacheWrite = resp.Usage.PromptTokensDetails.CacheWriteTokens
			details.PromptTokensAudio = resp.Usage.PromptTokensDetails.AudioTokens
			details.PromptTokensVideo = resp.Usage.PromptTokensDetails.VideoTokens
			log.CachedTokens = details.PromptTokensCached
		}

		if resp.Usage.CompletionTokensDetails != nil {
			details.CompletionTokensReasoning = resp.Usage.CompletionTokensDetails.ReasoningTokens
			details.CompletionTokensImage = resp.Usage.CompletionTokensDetails.ImageTokens
		}

		if resp.Usage.ServerToolUse != nil {
			details.WebSearchRequests = resp.Usage.ServerToolUse.WebSearchRequests
		}

		if resp.Usage.CostDetails != nil {
			details.UpstreamPromptCostMicros = int64(resp.Usage.CostDetails.UpstreamInferencePromptCost * 1000000)
			details.UpstreamCompletionCostMicros = int64(resp.Usage.CostDetails.UpstreamInferenceCompletionCost * 1000000)
			if resp.Usage.CostDetails.UpstreamInferenceCost != nil {
				cost := int64(*resp.Usage.CostDetails.UpstreamInferenceCost * 1000000)
				details.UpstreamCostMicros = &cost
			}
		}

		if resp.Usage.IsBYOK != nil {
			details.IsBYOK = *resp.Usage.IsBYOK
		}

		log.UsageDetails = details
	}

	pricing, err := s.repo.Providers().GetModelPricing(context.Background(), req.Model)
	if err == nil && pricing != nil && resp.Usage != nil {
		inputCost := (int64(resp.Usage.PromptTokens) * pricing.InputCostMicrosPer1k) / 1000
		outputCost := (int64(resp.Usage.CompletionTokens) * pricing.OutputCostMicrosPer1k) / 1000
		log.TotalCostMicros = inputCost + outputCost

		if log.UsageDetails != nil {
			log.UsageDetails.CostMicros = &log.TotalCostMicros
		}
	}

	s.ingestor.Log(log)

	return resp, nil
}

// GetProviderForModel resolves a public model ID via the catalog and returns
// the matching provider instance plus the upstream model ID to send on the
// wire. If the catalog knows the model but no active provider matches the
// catalog's claim, we return a ProviderError (500) because the config is
// internally inconsistent; missing models return a BadRequestError (400).
func (s *service) GetProviderForModel(ctx context.Context, modelID string) (llm.Provider, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 1. Try exact match in static registry
	if m, ok := s.models[modelID]; ok {
		p, exists := s.providers[m.ProviderID]
		if exists {
			upstreamID := m.UpstreamID
			if upstreamID == "" {
				upstreamID = m.ID
			}
			return p, upstreamID, nil
		}
		return nil, "", api.ProviderError(fmt.Sprintf("provider '%s' configured but not active/loaded", m.ProviderID), nil)
	}

	// 2. Permissive Pass-Through: check if the prefix matches a loaded provider
	parts := strings.SplitN(modelID, "/", 2)
	if len(parts) == 2 {
		providerID := parts[0]
		upstreamID := parts[1]

		if p, exists := s.providers[providerID]; exists {
			s.logger.Debug("using permissive pass-through for unlisted model",
				zap.String("model_id", modelID),
				zap.String("provider", providerID),
				zap.String("upstream_id", upstreamID),
			)
			return p, upstreamID, nil
		}
	}

	return nil, "", api.BadRequestError(fmt.Sprintf("route resolution failed for model '%s': not found in models and no matching provider prefix", modelID))
}

func (s *service) GetProvider(providerID string) (llm.Provider, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if p, exists := s.providers[providerID]; exists {
		return p, nil
	}

	return nil, api.ProviderError(fmt.Sprintf("provider '%s' configured but not active/loaded", providerID), nil)
}

func (s *service) CreateSpeech(ctx context.Context, req *api.SpeechRequest) (*api.SpeechResponse, error) {
	var audio bytes.Buffer
	contentType := ""
	if err := s.StreamSpeech(ctx, req, func(nextContentType string, chunk []byte) error {
		if contentType == "" {
			contentType = nextContentType
		}
		_, err := audio.Write(chunk)
		return err
	}); err != nil {
		return nil, err
	}
	return &api.SpeechResponse{
		Data:         audio.Bytes(),
		ContentType:  contentType,
		GenerationID: GenerationID(ctx),
	}, nil
}

func (s *service) StreamSpeech(ctx context.Context, req *api.SpeechRequest, write api.SpeechStreamWriter) error {
	provider, upstreamID, err := s.GetProviderForModel(ctx, req.Model)
	if err != nil {
		return err
	}
	format := req.ResponseFormat
	if format == "" {
		format = "pcm"
	}

	upstreamReq := &api.UpstreamSpeechRequest{
		Input:          req.Input,
		Model:          upstreamID,
		Voice:          req.Voice,
		ResponseFormat: format,
		Speed:          req.Speed,
		Provider:       req.Provider,
	}

	if speechProvider, ok := provider.(llm.SpeechProvider); ok {
		return speechProvider.StreamSpeech(ctx, upstreamReq, write)
	}

	return api.ProviderError(fmt.Sprintf("provider '%s' does not support /audio/speech", provider.Name()), nil)
}

func (s *service) StreamChat(ctx context.Context, req *api.ChatRequest) (<-chan api.StreamResult, error) {
	provider, upstreamID, err := s.GetProviderForModel(ctx, req.Model)
	if err != nil {
		logger.Warn("Provider routing failed for stream", zap.String("model", req.Model), zap.Error(err))
		return nil, err
	}

	reqClone := *req
	s.applyModelParameterPolicy(&reqClone, req.Model)
	reqClone.Model = upstreamID

	// Single source of truth for streaming. The pipeline forwards provider
	// chunks unchanged when no extensions/plugins are bound (zero overhead),
	// and runs the agentic tool-calling loop in-stream when they are.
	orchestrator := NewPipelineOrchestrator(s.plugins, s.extensions)
	streamChan, err := orchestrator.Stream(ctx, &reqClone, provider)
	if err != nil {
		return nil, err
	}

	// Intercept stream for logging
	outChan := make(chan api.StreamResult)

	go func() {
		defer close(outChan)

		start := time.Now()
		var ttft *time.Duration
		var inputTokens, outputTokens int
		var finalUsage *api.ResponseUsage
		var finishReason string
		var lastID string

		// Capture identity context before loop (context might be cancelled but values persist)
		var userID, apiKeyID, appName string
		if val, ok := ctx.Value(store.ContextKeyAppName).(string); ok {
			appName = val
		}
		if apiKey, ok := ctx.Value(store.ContextKeyAPIKey).(*model.APIKey); ok {
			userID = apiKey.UserID
			apiKeyID = apiKey.ID
		} else {
			if appName != "" {
				userID = string(api.Anonymous)
				apiKeyID = string(api.Anonymous)
			} else {
				userID = string(api.System)
				apiKeyID = string(api.System)
			}
		}

		genID := GenerationID(ctx)
		for result := range streamChan {
			// Record TTFT on first successful token
			if ttft == nil && result.Response != nil {
				dur := time.Since(start)
				ttft = &dur
			}

			if result.Response != nil {
				if result.Response.ID != "" {
					lastID = result.Response.ID
				}
				// Stamp the gateway-issued generation id on every chunk so
				// the response body always agrees with the X-Generation-Id
				// header (regardless of what the upstream provider chose).
				if genID != "" {
					result.Response.ID = genID
				}

				// Capture usage if provided (some providers send it in last chunk)
				if result.Response.Usage != nil {
					inputTokens = result.Response.Usage.PromptTokens
					outputTokens = result.Response.Usage.CompletionTokens
					finalUsage = result.Response.Usage
				}

				// If choices present
				if len(result.Response.Choices) > 0 {
					if result.Response.Choices[0].FinishReason != "" {
						finishReason = result.Response.Choices[0].FinishReason
					}
				}
			}

			select {
			case outChan <- result:
			case <-ctx.Done():
				// Stop sending tokens if client disconnected
				goto finalize
			}
		}

	finalize:
		// Log after stream closes
		latency := time.Since(start)
		var ttftMS sql.NullInt64
		if ttft != nil {
			ttftMS = sql.NullInt64{Int64: ttft.Milliseconds(), Valid: true}
		}

		statusCode := 200
		if ctx.Err() != nil {
			statusCode = 499
			if finishReason == "" {
				finishReason = "canceled"
			}
		}

		// Prefer the request-scoped generation id (matches X-Generation-Id
		// header + every chunk's `id` field). Fall back to the upstream id
		// if for some reason the gateway didn't assign one.
		logID := genID
		if logID == "" {
			logID = lastID
		}
		log := &model.RequestLog{
			ID:               logID,
			UserID:           userID,
			APIKeyID:         apiKeyID,
			AppName:          appName,
			ProviderID:       provider.Name(),
			ModelID:          req.Model,
			UpstreamModelID:  upstreamID,
			UpstreamRemoteID: lastID,
			FinishReason:     finishReason,
			StatusCode:       statusCode,
			LatencyMS:        latency.Milliseconds(),
			TTFTMS:           ttftMS,
			IsStreamed:       true,
			CreatedAt:        time.Now(),
			InputTokens:      inputTokens,
			OutputTokens:     outputTokens,
		}

		if finalUsage != nil {
			details := &model.UsageDetails{}
			if finalUsage.PromptTokensDetails != nil {
				details.PromptTokensCached = finalUsage.PromptTokensDetails.CachedTokens
				details.PromptTokensCacheWrite = finalUsage.PromptTokensDetails.CacheWriteTokens
				details.PromptTokensAudio = finalUsage.PromptTokensDetails.AudioTokens
				details.PromptTokensVideo = finalUsage.PromptTokensDetails.VideoTokens
				log.CachedTokens = details.PromptTokensCached
			}
			if finalUsage.CompletionTokensDetails != nil {
				details.CompletionTokensReasoning = finalUsage.CompletionTokensDetails.ReasoningTokens
				details.CompletionTokensImage = finalUsage.CompletionTokensDetails.ImageTokens
			}
			if finalUsage.ServerToolUse != nil {
				details.WebSearchRequests = finalUsage.ServerToolUse.WebSearchRequests
			}
			if finalUsage.CostDetails != nil {
				details.UpstreamPromptCostMicros = int64(finalUsage.CostDetails.UpstreamInferencePromptCost * 1000000)
				details.UpstreamCompletionCostMicros = int64(finalUsage.CostDetails.UpstreamInferenceCompletionCost * 1000000)
				if finalUsage.CostDetails.UpstreamInferenceCost != nil {
					cost := int64(*finalUsage.CostDetails.UpstreamInferenceCost * 1000000)
					details.UpstreamCostMicros = &cost
				}
			}
			if finalUsage.IsBYOK != nil {
				details.IsBYOK = *finalUsage.IsBYOK
			}
			log.UsageDetails = details
		}

		if log.ID == "" {
			log.ID = fmt.Sprintf("stream-fail-%d", time.Now().UnixNano())
			log.StatusCode = 500
		}

		// Calculate cost
		pricing, err := s.repo.Providers().GetModelPricing(context.Background(), req.Model)
		if err == nil && pricing != nil {
			inputCost := (int64(inputTokens) * pricing.InputCostMicrosPer1k) / 1000
			outputCost := (int64(outputTokens) * pricing.OutputCostMicrosPer1k) / 1000
			log.TotalCostMicros = inputCost + outputCost

			if log.UsageDetails != nil {
				log.UsageDetails.CostMicros = &log.TotalCostMicros
			}
		}

		s.ingestor.Log(log)
	}()

	return outChan, nil
}

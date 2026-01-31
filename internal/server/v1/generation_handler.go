package v1

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/store"
	"github.com/nulzo/model-router-api/internal/store/model"
	"github.com/nulzo/model-router-api/pkg/api"
)

type GenerationHandler struct {
	repo store.Repository
}

func NewGenerationHandler(repo store.Repository) *GenerationHandler {
	return &GenerationHandler{repo: repo}
}

func (h *GenerationHandler) GetGeneration(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id parameter is required"})
		return
	}

	// Validate Auth? The spec says Authorization required.
	// For now, we assume middleware handles general auth, but we might want to check
	// if the user owns this generation or is admin.
	// Getting user from context:
	user, _ := c.Get("user") // Assuming auth middleware populates this
	_ = user                 // TODO: Check ownership

	log, err := h.repo.Requests().GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Generation not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	response := mapRequestLogToGenerationResponse(log)
	c.JSON(http.StatusOK, response)
}

func mapRequestLogToGenerationResponse(log *model.RequestLog) api.GenerationResponse {
	totalCostUSD := float64(log.TotalCostMicros) / 1_000_000.0

	data := api.GenerationData{
		ID:                 log.ID,
		UpstreamID:         log.UpstreamRemoteID,
		TotalCost:          totalCostUSD,
		CreatedAt:          log.CreatedAt,
		Model:              log.ModelID,
		AppID:              log.AppName,
		Streamed:           log.IsStreamed,
		ProviderName:       log.ProviderID,
		Latency:            float64(log.LatencyMS),
		GenerationTime:     float64(log.LatencyMS), // Approx
		FinishReason:       log.FinishReason,
		TokensPrompt:       log.InputTokens,
		TokensCompletion:   log.OutputTokens,
		NativeTokensPrompt: log.InputTokens, // Default unless details
		NativeTokensCompletion: log.OutputTokens, // Default unless details
		Usage:              totalCostUSD,
		APIType:            "chat",
		Router:             "model-router",
		NativeFinishReason: log.FinishReason,
	}

	if log.UsageDetails != nil {
		data.IsBYOK = log.UsageDetails.IsBYOK
		
		if log.UsageDetails.UpstreamCostMicros != nil {
			cost := float64(*log.UsageDetails.UpstreamCostMicros) / 1_000_000.0
			data.UpstreamInferenceCost = &cost
		}

		// Update natives with details
		data.NativeTokensCached = &log.UsageDetails.PromptTokensCached
		data.NativeTokensReasoning = &log.UsageDetails.CompletionTokensReasoning
		
		// numAudio := log.UsageDetails.PromptTokensAudio 
		// This is tokens, not count, but spec asks for NumInputAudioPrompt (count)
		// We stored audio *tokens* not count. We can't map count 1:1 if we didn't store it.
		// For now we assume 0 or null if unknown, or maybe we stored it in meta?
		// Spec says "Number of audio inputs". We have "prompt_tokens_audio".
		// We'll leave it null or 0.
		
		numSearch := log.UsageDetails.WebSearchRequests
		data.NumSearchResults = &numSearch
	}

	return api.GenerationResponse{Data: data}
}

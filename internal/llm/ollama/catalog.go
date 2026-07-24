package ollama

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/nulzo/model-router-api/internal/httpclient"
	"github.com/nulzo/model-router-api/pkg/api"
)

type tagsResponse struct {
	Models []tagModel `json:"models"`
}

type tagModel struct {
	Name       string         `json:"name"`
	Model      string         `json:"model"`
	ModifiedAt time.Time      `json:"modified_at"`
	Details    *modelDetails  `json:"details"`
}

type modelDetails struct {
	Format          string   `json:"format"`
	Family          string   `json:"family"`
	Families        []string `json:"families"`
	ParameterSize   string   `json:"parameter_size"`
	Quantization    string   `json:"quantization_level"`
}

var defaultSupportedParameters = []string{
	"max_tokens",
	"response_format",
	"stop",
	"temperature",
	"tool_choice",
	"tools",
	"top_p",
}

// Models discovers locally installed Ollama models via GET /api/tags.
func (a *Adapter) Models(ctx context.Context) ([]api.ModelDefinition, error) {
	url := fmt.Sprintf("%s/api/tags", rootURL(a.config.BaseURL))

	var resp tagsResponse
	if err := httpclient.SendRequest(ctx, a.client, "GET", url, nil, nil, &resp); err != nil {
		return nil, fmt.Errorf("ollama model discovery failed: %w", err)
	}

	out := make([]api.ModelDefinition, 0, len(resp.Models))
	for _, m := range resp.Models {
		upstreamID := strings.TrimSpace(m.Name)
		if upstreamID == "" {
			upstreamID = strings.TrimSpace(m.Model)
		}
		if upstreamID == "" || shouldExcludeModel(upstreamID, m.Details, a.config.Config) {
			continue
		}
		out = append(out, buildModelDefinition(a.config.ID, upstreamID, m.Details))
	}
	return out, nil
}

func buildModelDefinition(providerID, upstreamID string, details *modelDetails) api.ModelDefinition {
	publicID := fmt.Sprintf("%s/%s", providerID, upstreamID)
	return api.ModelDefinition{
		ID:            publicID,
		Name:          upstreamID,
		ProviderID:    providerID,
		UpstreamID:    upstreamID,
		CanonicalSlug: publicID,
		Enabled:       true,
		Source:        "auto",
		Pricing: api.ModelPricing{
			Prompt:     "0",
			Completion: "0",
			Request:    "0",
			Image:      "0",
		},
		Architecture: api.ModelArchitecture{
			InputModalities:  []string{"text"},
			OutputModalities: []string{"text"},
			Tokenizer:        inferTokenizer(details),
		},
		SupportedParameters: append([]string(nil), defaultSupportedParameters...),
		TopProvider: api.ModelTopProvider{
			IsModerated: false,
		},
	}
}

func inferTokenizer(details *modelDetails) string {
	if details == nil {
		return "llama"
	}
	family := strings.ToLower(strings.TrimSpace(details.Family))
	switch {
	case strings.Contains(family, "qwen"):
		return "qwen2"
	case strings.Contains(family, "phi"):
		return "phi2"
	case family != "":
		return family
	}
	for _, f := range details.Families {
		f = strings.ToLower(strings.TrimSpace(f))
		switch {
		case strings.Contains(f, "qwen"):
			return "qwen2"
		case strings.Contains(f, "phi"):
			return "phi2"
		case f != "":
			return f
		}
	}
	return "llama"
}

func shouldExcludeModel(name string, details *modelDetails, cfg map[string]string) bool {
	lowerName := strings.ToLower(name)
	if strings.Contains(lowerName, "embed") {
		return true
	}
	if details != nil {
		family := strings.ToLower(details.Family)
		if family == "bert" || strings.Contains(family, "embed") {
			return true
		}
	}
	if raw, ok := cfg["exclude_name_patterns"]; ok {
		for _, pattern := range strings.Split(raw, ",") {
			pattern = strings.TrimSpace(strings.ToLower(pattern))
			if pattern == "" {
				continue
			}
			if matched, _ := path.Match(pattern, lowerName); matched {
				return true
			}
		}
	}
	return false
}

package modeldata

import "github.com/nulzo/model-router-api/pkg/schema"

var KnownModels = map[string]schema.Model{
	// OpenAI
	"gpt-4o": {
		Name:          "GPT-4o",
		Description:   "GPT-4o is OpenAI's fastest and most capable model.",
		ContextLength: 128000,
		Architecture: schema.Architecture{
			Modality:  "text+image->text",
			Tokenizer: "o200k_base",
		},
		Pricing: schema.Pricing{
			Prompt:     "0.000005",
			Completion: "0.000015",
		},
		TopProvider: schema.TopProvider{
			ContextLength: 128000,
			IsModerated:   true,
		},
	},
	"gpt-4-turbo": {
		Name:          "GPT-4 Turbo",
		Description:   "The latest GPT-4 model with improved instruction following, JSON mode, reproducible outputs, parallel function calling, and more.",
		ContextLength: 128000,
		Architecture: schema.Architecture{
			Modality:  "text->text",
			Tokenizer: "cl100k_base",
		},
		Pricing: schema.Pricing{
			Prompt:     "0.00001",
			Completion: "0.00003",
		},
		TopProvider: schema.TopProvider{
			ContextLength: 128000,
			IsModerated:   true,
		},
	},
	"gpt-3.5-turbo": {
		Name:          "GPT-3.5 Turbo",
		Description:   "The most cost-effective and capable model in the GPT-3.5 family.",
		ContextLength: 16385,
		Architecture: schema.Architecture{
			Modality:  "text->text",
			Tokenizer: "cl100k_base",
		},
		Pricing: schema.Pricing{
			Prompt:     "0.0000005",
			Completion: "0.0000015",
		},
		TopProvider: schema.TopProvider{
			ContextLength: 16385,
			IsModerated:   true,
		},
	},

	// Anthropic
	"claude-3-5-sonnet-20240620": {
		Name:          "Claude 3.5 Sonnet",
		Description:   "Anthropic's most intelligent model.",
		ContextLength: 200000,
		Architecture: schema.Architecture{
			Modality:  "text+image->text",
			Tokenizer: "claude",
		},
		Pricing: schema.Pricing{
			Prompt:     "0.000003",
			Completion: "0.000015",
		},
		TopProvider: schema.TopProvider{
			ContextLength: 200000,
			IsModerated:   false,
		},
	},
	"claude-3-opus-20240229": {
		Name:          "Claude 3 Opus",
		Description:   "Powerful model for highly complex tasks.",
		ContextLength: 200000,
		Architecture: schema.Architecture{
			Modality:  "text+image->text",
			Tokenizer: "claude",
		},
		Pricing: schema.Pricing{
			Prompt:     "0.000015",
			Completion: "0.000075",
		},
		TopProvider: schema.TopProvider{
			ContextLength: 200000,
			IsModerated:   false,
		},
	},
	"claude-3-haiku-20240307": {
		Name:          "Claude 3 Haiku",
		Description:   "Fast and compact model for near-instant responsiveness.",
		ContextLength: 200000,
		Architecture: schema.Architecture{
			Modality:  "text+image->text",
			Tokenizer: "claude",
		},
		Pricing: schema.Pricing{
			Prompt:     "0.00000025",
			Completion: "0.00000125",
		},
		TopProvider: schema.TopProvider{
			ContextLength: 200000,
			IsModerated:   false,
		},
	},

	// Gemini
	"gemini-1.5-pro": {
		Name:          "Gemini 1.5 Pro",
		Description:   "Mid-size multimodal model that scales across a wide range of tasks.",
		ContextLength: 2000000,
		Architecture: schema.Architecture{
			Modality:  "text+image+video->text",
			Tokenizer: "gemini",
		},
		Pricing: schema.Pricing{
			Prompt:     "0.0000035",
			Completion: "0.0000105",
		},
		TopProvider: schema.TopProvider{
			ContextLength: 2000000,
			IsModerated:   true,
		},
	},
	"gemini-1.5-flash": {
		Name:          "Gemini 1.5 Flash",
		Description:   "Fast and versatile multimodal model for scaling across diverse tasks.",
		ContextLength: 1000000,
		Architecture: schema.Architecture{
			Modality:  "text+image+video->text",
			Tokenizer: "gemini",
		},
		Pricing: schema.Pricing{
			Prompt:     "0.00000035",
			Completion: "0.00000105",
		},
		TopProvider: schema.TopProvider{
			ContextLength: 1000000,
			IsModerated:   true,
		},
	},
}

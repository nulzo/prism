package google

import (
	"testing"

	"github.com/nulzo/model-router-api/pkg/api"
	"github.com/stretchr/testify/assert"
)

func TestShape_ReferenceImage(t *testing.T) {
	// 1. Create a request with text + reference image (base64)
	req := &api.ChatRequest{
		Model: "gemini-2.5-flash-image",
		Messages: []api.ChatMessage{
			{
				Role: "user",
				Content: api.Content{
					Parts: []api.ContentPart{
						{
							Type: "text",
							Text: "Turn this sketch into a realistic image.",
						},
						{
							Type: "image_url",
							ImageURL: &api.ImageURL{
								// Simple 1x1 red pixel png
								URL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
							},
						},
					},
				},
			},
		},
		Modalities: []string{"image", "text"},
		Temperature: 0.7,
	}

	// 2. Call Shape
	geminiReq, err := Shape(req)
	assert.NoError(t, err)

	// 3. Verify Generation Config (Modalities)
	assert.NotNil(t, geminiReq.GenerationConfig)
	assert.Contains(t, geminiReq.GenerationConfig.ResponseModalities, "IMAGE")
	assert.Contains(t, geminiReq.GenerationConfig.ResponseModalities, "TEXT")
	assert.Equal(t, 0.7, geminiReq.GenerationConfig.Temperature)

	// 4. Verify Content (Text + Image)
	assert.Len(t, geminiReq.Contents, 1)
	content := geminiReq.Contents[0]
	assert.Equal(t, "user", content.Role)
	assert.Len(t, content.Parts, 2)

	// Part 1: Text
	assert.Equal(t, "Turn this sketch into a realistic image.", content.Parts[0].Text)

	// Part 2: Image
	assert.Empty(t, content.Parts[1].Text) // Should be empty for image part
	assert.NotNil(t, content.Parts[1].InlineData)
	assert.Equal(t, "image/png", content.Parts[1].InlineData.MimeType)
	assert.NotEmpty(t, content.Parts[1].InlineData.Data)
}

func TestShape_SimpleText(t *testing.T) {
	req := &api.ChatRequest{
		Model: "gemini-pro",
		Messages: []api.ChatMessage{
			{
				Role: "user",
				Content: api.Content{
					Text: "Hello!",
				},
			},
		},
	}

	geminiReq, err := Shape(req)
	assert.NoError(t, err)
	
	assert.Len(t, geminiReq.Contents, 1)
	assert.Equal(t, "user", geminiReq.Contents[0].Role)
	assert.Equal(t, "Hello!", geminiReq.Contents[0].Parts[0].Text)
	// No generation config if not specified
	assert.Nil(t, geminiReq.GenerationConfig)
}

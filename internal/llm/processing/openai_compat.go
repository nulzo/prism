package processing

import (
	"encoding/json"

	"github.com/nulzo/model-router-api/pkg/api"
)

// CompatMessage maps reasoning -> reasoning_content and ensures assistant tool
// calls always include non-empty reasoning_content for strict thinking models.
type CompatMessage struct {
	Message                  api.ChatMessage
	MissingToolCallReasoning string
}

func (m CompatMessage) MarshalJSON() ([]byte, error) {
	type wireMessage api.ChatMessage
	raw, err := json.Marshal(wireMessage(m.Message))
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}

	var rc *string
	if m.Message.Reasoning != "" {
		rc = &m.Message.Reasoning
	} else if m.Message.Role == "assistant" && len(m.Message.ToolCalls) > 0 {
		placeholder := m.MissingToolCallReasoning
		if placeholder == "" {
			placeholder = " "
		}
		rc = &placeholder
	}

	delete(obj, "reasoning")
	if rc != nil {
		reasoningContent, err := json.Marshal(*rc)
		if err != nil {
			return nil, err
		}
		obj["reasoning_content"] = reasoningContent
	}
	return json.Marshal(obj)
}

// FormatOpenAIMessages takes a slice of API chat messages and converts them to CompatMessage
// which ensures reasoning content is properly formatted for OpenAI-compatible APIs.
func FormatOpenAIMessages(messages []api.ChatMessage) []CompatMessage {
	compatMsgs := make([]CompatMessage, len(messages))
	for i, m := range messages {
		compatMsgs[i] = CompatMessage{Message: m}
	}
	return compatMsgs
}

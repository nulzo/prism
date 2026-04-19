package extension

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nulzo/model-router-api/pkg/api"
)

// DatetimeExtension provides the current date and time to the LLM.
type DatetimeExtension struct{}

func NewDatetimeExtension() *DatetimeExtension {
	return &DatetimeExtension{}
}

func (t *DatetimeExtension) Name() string {
	return "prism:datetime"
}

func (t *DatetimeExtension) BuildTool(config api.ExtensionConfig) (api.Tool, error) {
	return api.Tool{
		Type: "function",
		Function: api.FunctionDescription{
			Name:        t.Name(),
			Description: "Get the current date and time.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"timezone": map[string]interface{}{
						"type":        "string",
						"description": "IANA timezone name (e.g. 'America/New_York'). Defaults to UTC.",
					},
				},
			},
		},
	}, nil
}

type datetimeArgs struct {
	Timezone string `json:"timezone"`
}

func (t *DatetimeExtension) Execute(ctx context.Context, config api.ExtensionConfig, args []byte) (string, error) {
	var parsedArgs datetimeArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &parsedArgs); err != nil {
			return "", err
		}
	}

	loc := time.UTC
	if parsedArgs.Timezone == "" && config.Config != nil {
		if tz, ok := config.Config["default_timezone"].(string); ok {
			parsedArgs.Timezone = tz
		}
	}
	if parsedArgs.Timezone != "" {
		if l, err := time.LoadLocation(parsedArgs.Timezone); err == nil {
			loc = l
		}
	}

	now := time.Now().In(loc)
	result := map[string]string{
		"datetime": now.Format(time.RFC3339),
		"timezone": loc.String(),
	}

	resBytes, _ := json.Marshal(result)
	return string(resBytes), nil
}

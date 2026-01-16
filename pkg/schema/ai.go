package schema

type Role string

const (
	User           Role = "user"
	Assistant      Role = "assistant"
	System         Role = "system"
	ModelAssistant Role = "model"
)

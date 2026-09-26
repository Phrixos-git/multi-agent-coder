package mcp

import (
	"github.com/arietis/multi-agent-coder/internal/agent"
	"github.com/arietis/multi-agent-coder/internal/llm"
)

// agentJSONSchema constrains the Coding LLM response before existing output
// validation. The role is selected by the backend resolver, not the LLM client.
func agentJSONSchema(role agent.Role) llm.ResponseFormat {
	stringArray := func() map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	}
	objectArray := func(properties map[string]any, required []string) map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "properties": properties, "required": required, "additionalProperties": false,
		}}
	}
	return llm.ResponseFormat{Type: "json_schema", JSONSchema: &llm.JSONSchemaSpec{
		Name: "agent_result", Strict: true,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status":    map[string]any{"type": "string", "enum": []string{"success", "needs_input"}},
				"role":      map[string]any{"type": "string", "enum": []string{string(role)}},
				"summary":   map[string]any{"type": "string"},
				"artifacts": objectArray(map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, []string{"path", "content"}),
				"findings":  objectArray(map[string]any{"severity": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}}, []string{"severity", "description"}),
				"risks":     stringArray(),
				"notes":     stringArray(),
			},
			"required":             []string{"status", "role", "summary", "artifacts", "findings", "risks", "notes"},
			"additionalProperties": false,
		},
	}}
}

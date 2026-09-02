package llm

// ChatCompletionResponse is the validated subset of an OpenAI-compatible
// chat completion response used by the application.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Usage contains token counts reported by llama-server.
type Usage struct {
	CompletionTokens int `json:"completion_tokens"`
}

// Choice contains one generated assistant message.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

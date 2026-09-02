package llm

// Message is one message in an OpenAI-compatible chat conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionRequest is the request body accepted by llama-server's
// OpenAI-compatible chat completions endpoint.
type ChatCompletionRequest struct {
	Model           string         `json:"model"`
	Messages        []Message      `json:"messages"`
	MaxTokens       int            `json:"max_tokens"`
	ReasoningEffort string         `json:"reasoning_effort"`
	Temperature     float64        `json:"temperature"`
	ResponseFormat  ResponseFormat `json:"response_format"`
}

// ResponseFormat requests a structured JSON object from llama-server.
type ResponseFormat struct {
	Type string `json:"type"`
}

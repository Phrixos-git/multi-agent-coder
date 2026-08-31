package llm

// Message is one message in an OpenAI-compatible chat conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionRequest is the request body accepted by llama-server's
// OpenAI-compatible chat completions endpoint.
type ChatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

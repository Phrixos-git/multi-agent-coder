package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/arietis/multi-agent-coder/internal/config"
)

const (
	chatCompletionsPath = "/v1/chat/completions"
	maxResponseBytes    = 16 << 20
)

// Client calls llama-server's OpenAI-compatible API.
type Client struct {
	endpoint        string
	model           string
	timeout         time.Duration
	maxTokens       int
	reasoningEffort string
	temperature     float64
	httpClient      *http.Client
	logger          *slog.Logger
}

type roleOptions struct {
	maxTokens int
	timeout   time.Duration
}

var optionsByRole = map[string]roleOptions{
	"coder":    {maxTokens: 4096, timeout: 300 * time.Second},
	"reviewer": {maxTokens: 1536, timeout: 180 * time.Second},
	"tester":   {maxTokens: 1536, timeout: 180 * time.Second},
	"fixer":    {maxTokens: 4096, timeout: 300 * time.Second},
}

// NewClient constructs a client from application configuration.
func NewClient(cfg config.Config) *Client {
	return NewClientWithLogger(cfg, http.DefaultClient, slog.Default())
}

// NewClientWithHTTPClient constructs a client with a caller-provided HTTP
// client. It is primarily useful for tests and custom transports.
func NewClientWithHTTPClient(cfg config.Config, httpClient *http.Client) *Client {
	return NewClientWithLogger(cfg, httpClient, slog.Default())
}

// NewClientWithLogger constructs a client with explicit HTTP and logging dependencies.
func NewClientWithLogger(cfg config.Config, httpClient *http.Client, logger *slog.Logger) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		endpoint:        strings.TrimRight(cfg.LLMBaseURL, "/") + chatCompletionsPath,
		model:           cfg.LLMModel,
		timeout:         cfg.LLMTimeout,
		maxTokens:       cfg.LLMMaxTokens,
		reasoningEffort: cfg.LLMReasoningEffort,
		temperature:     cfg.LLMTemperature,
		httpClient:      httpClient,
		logger:          logger,
	}
}

// ChatCompletion sends messages to llama-server. A retryable failure is
// retried at most once; caller cancellation is never retried.
func (c *Client) ChatCompletion(ctx context.Context, messages []Message, roles ...string) (ChatCompletionResponse, error) {
	role := ""
	if len(roles) > 0 {
		role = roles[0]
	}
	options, ok := optionsByRole[role]
	if !ok {
		options = roleOptions{maxTokens: c.maxTokens, timeout: c.timeout}
	}
	payload, err := json.Marshal(ChatCompletionRequest{
		Model:           c.model,
		Messages:        messages,
		MaxTokens:       options.maxTokens,
		ReasoningEffort: c.reasoningEffort,
		Temperature:     c.temperature,
		ResponseFormat:  ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return ChatCompletionResponse{}, &Error{Kind: ErrorResponse, Err: fmt.Errorf("encode request: %w", err)}
	}

	for attempt := 0; attempt < 2; attempt++ {
		response, retry, err := c.do(ctx, payload, options.timeout)
		if err == nil {
			choice := response.Choices[0]
			c.logger.DebugContext(ctx, "LLM completion received", "role", role,
				"finish_reason", choice.FinishReason, "completion_tokens", response.Usage.CompletionTokens)
			if choice.FinishReason == "length" {
				return ChatCompletionResponse{}, &Error{Kind: ErrorTruncated, Err: errors.New("completion reached max_tokens")}
			}
			return response, nil
		}
		if !retry || attempt == 1 {
			return ChatCompletionResponse{}, err
		}
	}
	panic("unreachable")
}

func (c *Client) do(ctx context.Context, payload []byte, timeout time.Duration) (ChatCompletionResponse, bool, error) {
	if err := ctx.Err(); err != nil {
		return ChatCompletionResponse{}, false, classifyTransportError(err)
	}

	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return ChatCompletionResponse{}, false, &Error{Kind: ErrorConnection, Err: fmt.Errorf("create request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		classified := classifyTransportError(err)
		return ChatCompletionResponse{}, isRetryableTransport(err, ctx), classified
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		err := &Error{Kind: ErrorResponse, StatusCode: resp.StatusCode, Err: errors.New(resp.Status)}
		return ChatCompletionResponse{}, resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable, err
	}

	var result ChatCompletionResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err := decoder.Decode(&result); err != nil {
		return ChatCompletionResponse{}, false, &Error{Kind: ErrorResponse, Err: fmt.Errorf("decode response JSON: %w", err)}
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return ChatCompletionResponse{}, false, &Error{Kind: ErrorResponse, Err: err}
	}
	if len(result.Choices) == 0 {
		return ChatCompletionResponse{}, false, &Error{Kind: ErrorResponse, Err: errors.New("response contains no choices")}
	}
	if result.Choices[0].Message.Role == "" {
		return ChatCompletionResponse{}, false, &Error{Kind: ErrorResponse, Err: errors.New("response choice has no message role")}
	}
	if result.Choices[0].FinishReason == "" {
		return ChatCompletionResponse{}, false, &Error{Kind: ErrorResponse, Err: errors.New("response choice has no finish_reason")}
	}
	return result, false, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("response contains multiple JSON values")
		}
		return fmt.Errorf("decode response JSON: %w", err)
	}
	return nil
}

func classifyTransportError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Kind: ErrorTimeout, Err: err}
	}
	return &Error{Kind: ErrorConnection, Err: err}
}

func isRetryableTransport(err error, parent context.Context) bool {
	if parent.Err() != nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

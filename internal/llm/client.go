package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	endpoint   string
	model      string
	timeout    time.Duration
	httpClient *http.Client
}

// NewClient constructs a client from application configuration.
func NewClient(cfg config.Config) *Client {
	return NewClientWithHTTPClient(cfg, http.DefaultClient)
}

// NewClientWithHTTPClient constructs a client with a caller-provided HTTP
// client. It is primarily useful for tests and custom transports.
func NewClientWithHTTPClient(cfg config.Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		endpoint:   strings.TrimRight(cfg.LLMBaseURL, "/") + chatCompletionsPath,
		model:      cfg.LLMModel,
		timeout:    cfg.LLMTimeout,
		httpClient: httpClient,
	}
}

// ChatCompletion sends messages to llama-server. A retryable failure is
// retried at most once; caller cancellation is never retried.
func (c *Client) ChatCompletion(ctx context.Context, messages []Message) (ChatCompletionResponse, error) {
	payload, err := json.Marshal(ChatCompletionRequest{Model: c.model, Messages: messages})
	if err != nil {
		return ChatCompletionResponse{}, &Error{Kind: ErrorResponse, Err: fmt.Errorf("encode request: %w", err)}
	}

	for attempt := 0; attempt < 2; attempt++ {
		response, retry, err := c.do(ctx, payload)
		if err == nil {
			return response, nil
		}
		if !retry || attempt == 1 {
			return ChatCompletionResponse{}, err
		}
	}
	panic("unreachable")
}

func (c *Client) do(ctx context.Context, payload []byte) (ChatCompletionResponse, bool, error) {
	if err := ctx.Err(); err != nil {
		return ChatCompletionResponse{}, false, classifyTransportError(err)
	}

	attemptCtx, cancel := context.WithTimeout(ctx, c.timeout)
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

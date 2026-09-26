package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/arietis/multi-agent-coder/internal/config"
)

func TestChatCompletion(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != chatCompletionsPath {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		var request ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Model != "qwen-test" || len(request.Messages) != 1 || request.Messages[0].Content != "hello" || request.MaxTokens != 512 || request.ReasoningEffort != "none" || request.Temperature != 0.2 || request.ResponseFormat.Type != "json_object" {
			t.Errorf("request body = %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"completion-1","model":"qwen-test","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL, time.Second)
	response, err := client.ChatCompletion(context.Background(), []Message{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	if requests.Load() != 1 || response.Choices[0].Message.Content != "hi" {
		t.Fatalf("requests = %d, response = %+v", requests.Load(), response)
	}
}

func TestChatCompletionRetryableStatus(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if requests.Add(1) == 1 {
					http.Error(w, "temporary", status)
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer server.Close()

			_, err := newTestClient(server.URL, time.Second).ChatCompletion(context.Background(), nil)
			if err != nil || requests.Load() != 2 {
				t.Fatalf("error = %v, requests = %d", err, requests.Load())
			}
		})
	}
}

func TestChatCompletionUsesConfiguredLimitForAnyRole(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.MaxTokens != 512 || request.ResponseFormat.Type != "json_object" {
			t.Errorf("request = %+v", request)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	client := newTestClient(server.URL, time.Second)
	for _, role := range []string{"coder", "reviewer", "tester", "fixer"} {
		if _, err := client.ChatCompletion(context.Background(), nil, role); err != nil {
			t.Fatalf("ChatCompletion(%s) error = %v", role, err)
		}
	}
}

func TestChatCompletionLengthReturnsTruncatedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"status\":"},"finish_reason":"length"}],"usage":{"completion_tokens":4096}}`))
	}))
	defer server.Close()

	_, err := newTestClient(server.URL, time.Second).ChatCompletion(context.Background(), nil, "coder")
	var llmErr *Error
	if !errors.As(err, &llmErr) || llmErr.Kind != ErrorTruncated {
		t.Fatalf("error = %v, want LLMTruncatedError", err)
	}
}

func TestChatCompletionDoesNotRetryNonRetryableResponses(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantKind ErrorKind
	}{
		{name: "bad request", status: http.StatusBadRequest, body: `{"error":"context length exceeded"}`, wantKind: ErrorResponse},
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{}`, wantKind: ErrorResponse},
		{name: "not found", status: http.StatusNotFound, body: `{}`, wantKind: ErrorResponse},
		{name: "invalid JSON", status: http.StatusOK, body: `{`, wantKind: ErrorInvalidJSON},
		{name: "missing choices", status: http.StatusOK, body: `{}`, wantKind: ErrorResponse},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			_, err := newTestClient(server.URL, time.Second).ChatCompletion(context.Background(), nil)
			var llmErr *Error
			if !errors.As(err, &llmErr) || llmErr.Kind != test.wantKind || requests.Load() != 1 {
				t.Fatalf("error = %v, requests = %d", err, requests.Load())
			}
		})
	}
}

func TestChatCompletionRejectsMultipleJSONValues(t *testing.T) {
	valid := `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "second JSON value", body: valid + ` {}`},
		{name: "malformed trailing data", body: valid + ` trailing`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			_, err := newTestClient(server.URL, time.Second).ChatCompletion(context.Background(), nil)
			var llmErr *Error
			if !errors.As(err, &llmErr) || llmErr.Kind != ErrorInvalidJSON || requests.Load() != 1 {
				t.Fatalf("error = %v, requests = %d; want one non-retried LLMInvalidJSONError", err, requests.Load())
			}
		})
	}
}

func TestChatCompletionRetriesTimeoutOnce(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"late"}}]}`))
	}))
	defer server.Close()

	_, err := newTestClient(server.URL, 10*time.Millisecond).ChatCompletion(context.Background(), nil)
	var llmErr *Error
	if !errors.As(err, &llmErr) || llmErr.Kind != ErrorTimeout || requests.Load() != 2 {
		t.Fatalf("error = %v, requests = %d", err, requests.Load())
	}
}

func TestChatCompletionRetriesConnectionResetOnce(t *testing.T) {
	var requests atomic.Int32
	httpClient := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, syscall.ECONNRESET
	})}
	client := NewClientWithHTTPClient(config.Config{
		MainLLM: config.LLMConfig{BaseURL: "http://127.0.0.1:8080", Model: "qwen-test", Timeout: time.Second},
	}, httpClient)

	_, err := client.ChatCompletion(context.Background(), nil)
	var llmErr *Error
	if !errors.As(err, &llmErr) || llmErr.Kind != ErrorConnection || requests.Load() != 2 {
		t.Fatalf("error = %v, requests = %d", err, requests.Load())
	}
}

func TestChatCompletionDoesNotRetryCallerCancellation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newTestClient(server.URL, time.Second).ChatCompletion(ctx, nil)
	var llmErr *Error
	if !errors.As(err, &llmErr) || llmErr.Kind != ErrorConnection || requests.Load() != 0 {
		t.Fatalf("error = %v, requests = %d", err, requests.Load())
	}
}

func newTestClient(baseURL string, timeout time.Duration) *Client {
	return NewClient(config.Config{
		MainLLM:            config.LLMConfig{BaseURL: baseURL, Model: "qwen-test", Timeout: timeout},
		LLMMaxTokens:       512,
		LLMReasoningEffort: "none",
		LLMTemperature:     0.2,
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

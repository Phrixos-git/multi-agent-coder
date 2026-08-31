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
		if request.Model != "qwen-test" || len(request.Messages) != 1 || request.Messages[0].Content != "hello" {
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
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
			}))
			defer server.Close()

			_, err := newTestClient(server.URL, time.Second).ChatCompletion(context.Background(), nil)
			if err != nil || requests.Load() != 2 {
				t.Fatalf("error = %v, requests = %d", err, requests.Load())
			}
		})
	}
}

func TestChatCompletionDoesNotRetryNonRetryableResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "bad request", status: http.StatusBadRequest, body: `{"error":"context length exceeded"}`},
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{}`},
		{name: "not found", status: http.StatusNotFound, body: `{}`},
		{name: "invalid JSON", status: http.StatusOK, body: `{`},
		{name: "missing choices", status: http.StatusOK, body: `{}`},
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
			if !errors.As(err, &llmErr) || llmErr.Kind != ErrorResponse || requests.Load() != 1 {
				t.Fatalf("error = %v, requests = %d", err, requests.Load())
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
		LLMBaseURL: "http://127.0.0.1:8080",
		LLMModel:   "qwen-test",
		LLMTimeout: time.Second,
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
	return NewClient(config.Config{LLMBaseURL: baseURL, LLMModel: "qwen-test", LLMTimeout: timeout})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

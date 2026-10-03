package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arietis/multi-agent-coder/internal/config"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestProbePiRequiresExecutableAndAgentDirectory(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "pi")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg := config.Config{CoderBackend: "pi", PiExecutable: executable, PiAgentDir: dir, PiModel: "qwen"}
	if got := probePi(cfg); got.Status != "available" || got.Model != "qwen" {
		t.Fatalf("Pi readiness = %+v", got)
	}
	cfg.PiAgentDir = filepath.Join(t.TempDir(), "missing")
	if got := probePi(cfg); got.Status != "unavailable" {
		t.Fatalf("missing Pi directory readiness = %+v", got)
	}
}

func TestLocalAgentHealth(t *testing.T) {
	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("main path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer main.Close()
	coding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("coding path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer coding.Close()
	server, err := NewServer(config.Config{
		MainLLM:        config.LLMConfig{BaseURL: main.URL, Model: "main", Timeout: time.Second},
		CodingLLM:      config.LLMConfig{BaseURL: coding.URL, Model: "coding", Timeout: time.Second},
		RepositoryRoot: t.TempDir(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()

	ctx := context.Background()
	go func() { _ = server.SDKServer().Run(ctx, serverTransport) }()
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "local_agent_health"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	output, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent type = %T", result.StructuredContent)
	}
	mainHealth := output["main_llm"].(map[string]any)
	codingHealth := output["coding_llm"].(map[string]any)
	if output["status"] != "ok" || output["llm"] != "available" || mainHealth["status"] != "available" || mainHealth["base_url"] != main.URL || codingHealth["status"] != "available" || codingHealth["model"] != "coding" {
		t.Fatalf("StructuredContent = %#v", output)
	}
}

func TestProbeLLMDistinguishesStoppedAndFailed(t *testing.T) {
	stopped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := stopped.URL
	stopped.Close()
	if got := probeLLM(context.Background(), config.LLMConfig{BaseURL: url}); got.Status != "stopped" {
		t.Fatalf("stopped probe = %+v", got)
	}
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer failed.Close()
	if got := probeLLM(context.Background(), config.LLMConfig{BaseURL: failed.URL}); got.Status != "failed" {
		t.Fatalf("failed probe = %+v", got)
	}
}

func TestLocalAgentHealthKeepsServingWhenCodingLLMStopped(t *testing.T) {
	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer main.Close()
	coding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	codingURL := coding.URL
	coding.Close()
	server, err := NewServer(config.Config{
		MainLLM:        config.LLMConfig{BaseURL: main.URL, Timeout: time.Second},
		CodingLLM:      config.LLMConfig{BaseURL: codingURL, Timeout: time.Second},
		RepositoryRoot: t.TempDir(),
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "health-test", Version: "1"}, nil)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.SDKServer().Run(ctx, serverTransport) }()
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for range 2 {
		result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "local_agent_health"})
		if err != nil {
			t.Fatal(err)
		}
		out := result.StructuredContent.(map[string]any)
		if out["status"] != "degraded" || out["main_llm"].(map[string]any)["status"] != "available" || out["coding_llm"].(map[string]any)["status"] != "stopped" {
			t.Fatalf("health = %#v", out)
		}
	}
}

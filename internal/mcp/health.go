package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/arietis/multi-agent-coder/internal/config"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type healthInput struct{}

// HealthOutput is the structured result of local_agent_health.
type HealthOutput struct {
	Status    string    `json:"status" jsonschema:"MCP server status"`
	LLM       string    `json:"llm" jsonschema:"legacy main LLM status"`
	MainLLM   LLMHealth `json:"main_llm" jsonschema:"main LLM connectivity"`
	CodingLLM LLMHealth `json:"coding_llm" jsonschema:"coding LLM connectivity"`
}

type LLMHealth struct {
	Status  string `json:"status"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
}

func registerHealthTool(server *sdkmcp.Server, cfg config.Config, logger *slog.Logger) {
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "local_agent_health",
		Description: "Report Main LLM and Coding LLM health independently.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ healthInput) (*sdkmcp.CallToolResult, HealthOutput, error) {
		started := time.Now()
		requestID := newRequestID()
		output := HealthOutput{
			Status:    "ok",
			MainLLM:   probeLLM(ctx, cfg.MainLLM),
			CodingLLM: probeLLM(ctx, cfg.CodingLLM),
		}
		output.LLM = output.MainLLM.Status
		if output.MainLLM.Status != "available" || output.CodingLLM.Status != "available" {
			output.Status = "degraded"
		}
		logger.InfoContext(ctx, "MCP tool request completed",
			"role", "health",
			"request_id", requestID,
			"duration", time.Since(started),
			"status", output.Status,
		)
		return nil, output, nil
	})
}

// probeLLM keeps health calls short even when generation timeout is long.
func probeLLM(ctx context.Context, cfg config.LLMConfig) LLMHealth {
	result := LLMHealth{Status: "failed", BaseURL: cfg.BaseURL, Model: cfg.Model}
	if cfg.BaseURL == "" {
		result.Status = "unavailable"
		return result
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimRight(cfg.BaseURL, "/")+"/v1/models", nil)
	if err != nil {
		return result
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			result.Status = "stopped"
		} else if errors.Is(err, context.DeadlineExceeded) {
			result.Status = "failed"
		} else {
			var netErr net.Error
			if errors.As(err, &netErr) {
				result.Status = "unavailable"
			}
		}
		return result
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		result.Status = "available"
	}
	return result
}

func newRequestID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "unavailable"
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	var text [36]byte
	hex.Encode(text[0:8], id[0:4])
	text[8] = '-'
	hex.Encode(text[9:13], id[4:6])
	text[13] = '-'
	hex.Encode(text[14:18], id[6:8])
	text[18] = '-'
	hex.Encode(text[19:23], id[8:10])
	text[23] = '-'
	hex.Encode(text[24:36], id[10:16])
	return string(text[:])
}

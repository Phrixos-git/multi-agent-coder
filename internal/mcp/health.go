package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os/exec"
	"time"

	"github.com/arietis/multi-agent-coder/internal/config"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type healthInput struct{}

// HealthOutput is the structured result of local_agent_health.
type HealthOutput struct {
	Status string `json:"status" jsonschema:"MCP server status"`
	LLM    string `json:"llm" jsonschema:"local LLM status"`
	Pi     string `json:"pi,omitempty" jsonschema:"Pi executable status when configured"`
}

func registerHealthTool(server *sdkmcp.Server, cfg config.Config, logger *slog.Logger) {
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "local_agent_health",
		Description: "Report MCP server health. LLM connectivity is added in Phase 2.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ healthInput) (*sdkmcp.CallToolResult, HealthOutput, error) {
		started := time.Now()
		requestID := newRequestID()
		output := HealthOutput{Status: "ok", LLM: "not_checked"}
		if cfg.CoderBackend == "pi" {
			if _, err := exec.LookPath(cfg.PiExecutable); err != nil {
				output.Status = "degraded"
				output.Pi = "not_found"
			} else {
				output.Pi = "available"
			}
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

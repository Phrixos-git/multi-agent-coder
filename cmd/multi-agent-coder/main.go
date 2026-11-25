package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/arietis/multi-agent-coder/internal/config"
	mcpserver "github.com/arietis/multi-agent-coder/internal/mcp"
)

func main() {
	fromCWD := flag.Bool("workspace-from-cwd", false, "use the MCP launch directory as the session workspace")
	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*fromCWD); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(fromCWD bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.SlogLevel()}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	directory := workspaceDirectory(cfg.Orchestrator, fromCWD)
	server, err := mcpserver.NewServer(ctx, cfg, logger, directory)
	if err != nil {
		return fmt.Errorf("create MCP server: %w", err)
	}
	if err := server.Run(ctx); err != nil {
		return fmt.Errorf("run MCP server: %w", err)
	}
	return nil
}

func workspaceDirectory(orchestrator string, fromCWD bool) string {
	if orchestrator == config.OrchestratorClaudeCode {
		if directory := os.Getenv("CLAUDE_PROJECT_DIR"); directory != "" {
			return directory
		}
	}
	if fromCWD {
		directory, err := os.Getwd()
		if err == nil {
			return directory
		}
	}
	return ""
}

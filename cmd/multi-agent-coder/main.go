package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/arietis/multi-agent-coder/internal/config"
	mcpserver "github.com/arietis/multi-agent-coder/internal/mcp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.SlogLevel()}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	server := mcpserver.NewServer(cfg, logger)
	if err := server.Run(ctx); err != nil {
		return fmt.Errorf("run MCP server: %w", err)
	}
	return nil
}

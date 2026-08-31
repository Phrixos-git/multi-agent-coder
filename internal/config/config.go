package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultLLMBaseURL = "http://127.0.0.1:8080"
	defaultLLMTimeout = 120 * time.Second
	defaultLogLevel   = "info"
)

// Config contains process configuration loaded from MAC_* environment variables.
type Config struct {
	LLMBaseURL     string
	LLMModel       string
	LLMTimeout     time.Duration
	RepositoryRoot string
	LogLevel       string
}

// Load reads and validates configuration from the environment.
func Load() (Config, error) {
	repositoryRoot := strings.TrimSpace(os.Getenv("MAC_REPOSITORY_ROOT"))
	if repositoryRoot == "" {
		var err error
		repositoryRoot, err = os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("ConfigError: determine repository root: %w", err)
		}
	}

	timeout, err := parseDuration("MAC_LLM_TIMEOUT", defaultLLMTimeout)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		LLMBaseURL:     envOrDefault("MAC_LLM_BASE_URL", defaultLLMBaseURL),
		LLMModel:       strings.TrimSpace(os.Getenv("MAC_LLM_MODEL")),
		LLMTimeout:     timeout,
		RepositoryRoot: repositoryRoot,
		LogLevel:       strings.ToLower(envOrDefault("MAC_LOG_LEVEL", defaultLogLevel)),
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	u, err := url.ParseRequestURI(c.LLMBaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("ConfigError: MAC_LLM_BASE_URL must be an absolute HTTP(S) URL")
	}
	if c.LLMTimeout <= 0 {
		return fmt.Errorf("ConfigError: MAC_LLM_TIMEOUT must be greater than zero")
	}

	root, err := filepath.Abs(c.RepositoryRoot)
	if err != nil {
		return fmt.Errorf("ConfigError: resolve MAC_REPOSITORY_ROOT: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("ConfigError: inspect MAC_REPOSITORY_ROOT: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("ConfigError: MAC_REPOSITORY_ROOT is not a directory")
	}
	c.RepositoryRoot = root

	if _, ok := logLevels[c.LogLevel]; !ok {
		return fmt.Errorf("ConfigError: MAC_LOG_LEVEL must be debug, info, warn, or error")
	}
	return nil
}

var logLevels = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

// SlogLevel returns the configured level in the standard logger's representation.
func (c Config) SlogLevel() slog.Level { return logLevels[c.LogLevel] }

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func parseDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("ConfigError: parse %s: %w", name, err)
	}
	return duration, nil
}

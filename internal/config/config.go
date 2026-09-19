package config

import (
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLLMBaseURL         = "http://127.0.0.1:8080"
	defaultLLMTimeout         = 180 * time.Second
	defaultLLMMaxTokens       = 4096
	defaultCoderMaxTokens     = 4096
	defaultReviewerMaxTokens  = 1536
	defaultTesterMaxTokens    = 1536
	defaultFixerMaxTokens     = 4096
	defaultLLMReasoningEffort = "none"
	defaultLLMTemperature     = 0.2
	defaultLogLevel           = "info"
	defaultCoderBackend       = "direct"
	defaultPiExecutable       = "pi"
	defaultPiTimeout          = 300 * time.Second
)

// Config contains process configuration loaded from MAC_* environment variables.
type Config struct {
	LLMBaseURL         string
	LLMModel           string
	LLMTimeout         time.Duration
	LLMMaxTokens       int
	CoderMaxTokens     int
	ReviewerMaxTokens  int
	TesterMaxTokens    int
	FixerMaxTokens     int
	LLMReasoningEffort string
	LLMTemperature     float64
	RepositoryRoot     string
	LogLevel           string
	CoderBackend       string
	PiExecutable       string
	PiProvider         string
	PiModel            string
	PiRPCArgs          []string
	PiTimeout          time.Duration
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
	maxTokens, err := parseInt("MAC_LLM_MAX_TOKENS", defaultLLMMaxTokens)
	if err != nil {
		return Config{}, err
	}
	coderMaxTokens, err := parseInt("MAC_CODER_MAX_TOKENS", defaultCoderMaxTokens)
	if err != nil {
		return Config{}, err
	}
	reviewerMaxTokens, err := parseInt("MAC_REVIEWER_MAX_TOKENS", defaultReviewerMaxTokens)
	if err != nil {
		return Config{}, err
	}
	testerMaxTokens, err := parseInt("MAC_TESTER_MAX_TOKENS", defaultTesterMaxTokens)
	if err != nil {
		return Config{}, err
	}
	fixerMaxTokens, err := parseInt("MAC_FIXER_MAX_TOKENS", defaultFixerMaxTokens)
	if err != nil {
		return Config{}, err
	}
	temperature, err := parseFloat("MAC_LLM_TEMPERATURE", defaultLLMTemperature)
	if err != nil {
		return Config{}, err
	}
	piTimeout, err := parseDuration("MAC_PI_TIMEOUT", defaultPiTimeout)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		LLMBaseURL:         envOrDefault("MAC_LLM_BASE_URL", defaultLLMBaseURL),
		LLMModel:           strings.TrimSpace(os.Getenv("MAC_LLM_MODEL")),
		LLMTimeout:         timeout,
		LLMMaxTokens:       maxTokens,
		CoderMaxTokens:     coderMaxTokens,
		ReviewerMaxTokens:  reviewerMaxTokens,
		TesterMaxTokens:    testerMaxTokens,
		FixerMaxTokens:     fixerMaxTokens,
		LLMReasoningEffort: envOrDefault("MAC_LLM_REASONING_EFFORT", defaultLLMReasoningEffort),
		LLMTemperature:     temperature,
		RepositoryRoot:     repositoryRoot,
		LogLevel:           strings.ToLower(envOrDefault("MAC_LOG_LEVEL", defaultLogLevel)),
		CoderBackend:       strings.ToLower(envOrDefault("MAC_CODER_BACKEND", defaultCoderBackend)),
		PiExecutable:       envOrDefault("MAC_PI_EXECUTABLE", defaultPiExecutable),
		PiProvider:         strings.TrimSpace(os.Getenv("MAC_PI_PROVIDER")),
		PiModel:            strings.TrimSpace(os.Getenv("MAC_PI_MODEL")),
		PiRPCArgs:          strings.Fields(os.Getenv("MAC_PI_RPC_ARGS")),
		PiTimeout:          piTimeout,
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
	if c.LLMMaxTokens <= 0 {
		return fmt.Errorf("ConfigError: MAC_LLM_MAX_TOKENS must be greater than zero")
	}
	if c.CoderMaxTokens <= 0 {
		return fmt.Errorf("ConfigError: MAC_CODER_MAX_TOKENS must be greater than zero")
	}
	if c.ReviewerMaxTokens <= 0 {
		return fmt.Errorf("ConfigError: MAC_REVIEWER_MAX_TOKENS must be greater than zero")
	}
	if c.TesterMaxTokens <= 0 {
		return fmt.Errorf("ConfigError: MAC_TESTER_MAX_TOKENS must be greater than zero")
	}
	if c.FixerMaxTokens <= 0 {
		return fmt.Errorf("ConfigError: MAC_FIXER_MAX_TOKENS must be greater than zero")
	}
	if strings.TrimSpace(c.LLMReasoningEffort) == "" {
		return fmt.Errorf("ConfigError: MAC_LLM_REASONING_EFFORT must not be empty")
	}
	if math.IsNaN(c.LLMTemperature) || c.LLMTemperature < 0 || c.LLMTemperature > 2 {
		return fmt.Errorf("ConfigError: MAC_LLM_TEMPERATURE must be between 0 and 2")
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
	if c.CoderBackend != "direct" && c.CoderBackend != "pi" {
		return fmt.Errorf("ConfigError: MAC_CODER_BACKEND must be direct or pi")
	}
	if strings.TrimSpace(c.PiExecutable) == "" {
		return fmt.Errorf("ConfigError: MAC_PI_EXECUTABLE must not be empty")
	}
	if c.PiTimeout <= 0 {
		return fmt.Errorf("ConfigError: MAC_PI_TIMEOUT must be greater than zero")
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

func parseInt(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("ConfigError: parse %s: %w", name, err)
	}
	return parsed, nil
}

func parseFloat(name string, fallback float64) (float64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("ConfigError: parse %s: %w", name, err)
	}
	return parsed, nil
}

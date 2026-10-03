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
	defaultLLMModel           = "qwen3.8-27b"
	defaultCodingLLMBaseURL   = "http://127.0.0.1:18082"
	defaultCodingLLMModel     = "qwen-coder"
	defaultLLMTimeout         = 180 * time.Second
	defaultLLMMaxTokens       = 4096
	defaultCoderMaxTokens     = 4096
	defaultReviewerMaxTokens  = 4096
	defaultTesterMaxTokens    = 3072
	defaultFixerMaxTokens     = 4096
	defaultLLMReasoningEffort = "none"
	defaultLLMTemperature     = 0.2
	defaultLogLevel           = "info"
)

// LLMConfig identifies one OpenAI-compatible local model endpoint.
type LLMConfig struct {
	BaseURL string
	Model   string
	Timeout time.Duration
}

// Config contains process configuration loaded from MAC_* environment variables.
type Config struct {
	CoderBackend       string
	FixerBackend       string
	PiExecutable       string
	PiProvider         string
	PiModel            string
	PiAgentDir         string
	PiRPCArgs          string
	PiTimeout          time.Duration
	MainLLM            LLMConfig
	CodingLLM          LLMConfig
	LLMMaxTokens       int
	CoderMaxTokens     int
	ReviewerMaxTokens  int
	TesterMaxTokens    int
	FixerMaxTokens     int
	LLMReasoningEffort string
	LLMTemperature     float64
	RepositoryRoot     string
	LogLevel           string
}

// Load reads and validates configuration from the environment.
func Load() (Config, error) {
	piTimeout, err := parseDuration("MAC_PI_TIMEOUT", 0)
	if err != nil {
		return Config{}, err
	}
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
	codingTimeout, err := parseDuration("MAC_CODING_LLM_TIMEOUT", defaultLLMTimeout)
	if err != nil {
		return Config{}, err
	}
	maxTokens, err := parseInt("MAC_LLM_MAX_TOKENS", defaultLLMMaxTokens)
	if err != nil {
		return Config{}, err
	}
	coderMaxTokens, err := parseRoleMaxTokens("MAC_CODER_MAX_TOKENS", defaultCoderMaxTokens, maxTokens)
	if err != nil {
		return Config{}, err
	}
	reviewerMaxTokens, err := parseRoleMaxTokens("MAC_REVIEWER_MAX_TOKENS", defaultReviewerMaxTokens, maxTokens)
	if err != nil {
		return Config{}, err
	}
	testerMaxTokens, err := parseRoleMaxTokens("MAC_TESTER_MAX_TOKENS", defaultTesterMaxTokens, maxTokens)
	if err != nil {
		return Config{}, err
	}
	fixerMaxTokens, err := parseRoleMaxTokens("MAC_FIXER_MAX_TOKENS", defaultFixerMaxTokens, maxTokens)
	if err != nil {
		return Config{}, err
	}
	temperature, err := parseFloat("MAC_LLM_TEMPERATURE", defaultLLMTemperature)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		CoderBackend: strings.ToLower(envOrDefault("MAC_CODER_BACKEND", "direct")),
		FixerBackend: strings.ToLower(envOrDefault("MAC_FIXER_BACKEND", "direct")),
		PiExecutable: envOrDefault("MAC_PI_EXECUTABLE", "pi"),
		PiProvider:   envOrDefault("MAC_PI_PROVIDER", "local-qwen"),
		PiModel:      envOrDefault("MAC_PI_MODEL", "qwen"),
		PiAgentDir:   strings.TrimSpace(os.Getenv("MAC_PI_AGENT_DIR")),
		PiRPCArgs:    strings.TrimSpace(os.Getenv("MAC_PI_RPC_ARGS")),
		PiTimeout:    piTimeout,
		MainLLM: LLMConfig{
			BaseURL: envOrDefault("MAC_LLM_BASE_URL", defaultLLMBaseURL),
			Model:   envOrDefault("MAC_LLM_MODEL", defaultLLMModel), Timeout: timeout,
		},
		CodingLLM: LLMConfig{
			BaseURL: envOrDefault("MAC_CODING_LLM_BASE_URL", defaultCodingLLMBaseURL),
			Model:   envOrDefault("MAC_CODING_LLM_MODEL", defaultCodingLLMModel), Timeout: codingTimeout,
		},
		LLMMaxTokens:       maxTokens,
		CoderMaxTokens:     coderMaxTokens,
		ReviewerMaxTokens:  reviewerMaxTokens,
		TesterMaxTokens:    testerMaxTokens,
		FixerMaxTokens:     fixerMaxTokens,
		LLMReasoningEffort: envOrDefault("MAC_LLM_REASONING_EFFORT", defaultLLMReasoningEffort),
		LLMTemperature:     temperature,
		RepositoryRoot:     repositoryRoot,
		LogLevel:           strings.ToLower(envOrDefault("MAC_LOG_LEVEL", defaultLogLevel)),
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.CoderBackend != "direct" && c.CoderBackend != "pi" {
		return fmt.Errorf("ConfigError: MAC_CODER_BACKEND must be direct or pi")
	}
	if c.FixerBackend != "direct" && c.FixerBackend != "pi" {
		return fmt.Errorf("ConfigError: MAC_FIXER_BACKEND must be direct or pi")
	}
	if strings.TrimSpace(c.PiExecutable) == "" || strings.TrimSpace(c.PiProvider) == "" || strings.TrimSpace(c.PiModel) == "" {
		return fmt.Errorf("ConfigError: Pi executable, provider, and model must not be empty")
	}
	for _, arg := range strings.Fields(c.PiRPCArgs) {
		for _, protected := range []string{"--mode", "--tools", "-t", "--exclude-tools", "-xt", "--no-tools", "-nt", "--no-builtin-tools", "-nbt", "--no-extensions", "-ne", "--extension", "-e", "--provider", "--model", "--session", "--session-id", "--session-dir", "--continue", "-c", "--resume", "-r", "--fork"} {
			if arg == protected || strings.HasPrefix(arg, protected+"=") {
				return fmt.Errorf("ConfigError: MAC_PI_RPC_ARGS cannot override protected Pi option %s", protected)
			}
		}
	}
	if c.PiTimeout < 0 {
		return fmt.Errorf("ConfigError: MAC_PI_TIMEOUT must be zero (unlimited) or greater")
	}
	for _, backend := range []struct {
		name string
		cfg  LLMConfig
	}{{"MAC_LLM", c.MainLLM}, {"MAC_CODING_LLM", c.CodingLLM}} {
		u, err := url.ParseRequestURI(backend.cfg.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("ConfigError: %s_BASE_URL must be an absolute HTTP(S) URL", backend.name)
		}
		if backend.cfg.Timeout <= 0 {
			return fmt.Errorf("ConfigError: %s_TIMEOUT must be greater than zero", backend.name)
		}
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

func parseRoleMaxTokens(name string, codeDefault, global int) (int, error) {
	fallback := codeDefault
	if strings.TrimSpace(os.Getenv("MAC_LLM_MAX_TOKENS")) != "" {
		fallback = global
	}
	return parseInt(name, fallback)
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

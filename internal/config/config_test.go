package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("MAC_LLM_BASE_URL", "")
	t.Setenv("MAC_LLM_MODEL", "")
	t.Setenv("MAC_LLM_TIMEOUT", "")
	t.Setenv("MAC_REPOSITORY_ROOT", t.TempDir())
	t.Setenv("MAC_LOG_LEVEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLMBaseURL != defaultLLMBaseURL || cfg.LLMTimeout != 120*time.Second || cfg.LogLevel != "info" {
		t.Fatalf("Load() defaults = %+v", cfg)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		value string
	}{
		{name: "URL", env: "MAC_LLM_BASE_URL", value: "localhost:8080"},
		{name: "timeout", env: "MAC_LLM_TIMEOUT", value: "forever"},
		{name: "log level", env: "MAC_LOG_LEVEL", value: "verbose"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("MAC_REPOSITORY_ROOT", t.TempDir())
			t.Setenv(test.env, test.value)
			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil, want ConfigError")
			}
		})
	}
}

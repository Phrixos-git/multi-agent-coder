package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("MAC_LLM_BASE_URL", "")
	t.Setenv("MAC_LLM_MODEL", "")
	t.Setenv("MAC_LLM_TIMEOUT", "")
	t.Setenv("MAC_LLM_MAX_TOKENS", "")
	t.Setenv("MAC_CODER_MAX_TOKENS", "")
	t.Setenv("MAC_REVIEWER_MAX_TOKENS", "")
	t.Setenv("MAC_TESTER_MAX_TOKENS", "")
	t.Setenv("MAC_FIXER_MAX_TOKENS", "")
	t.Setenv("MAC_LLM_REASONING_EFFORT", "")
	t.Setenv("MAC_LLM_TEMPERATURE", "")
	t.Setenv("MAC_REPOSITORY_ROOT", t.TempDir())
	t.Setenv("MAC_LOG_LEVEL", "")
	t.Setenv("MAC_CODER_BACKEND", "")
	t.Setenv("MAC_PI_EXECUTABLE", "")
	t.Setenv("MAC_PI_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLMBaseURL != defaultLLMBaseURL || cfg.LLMTimeout != 180*time.Second || cfg.LLMMaxTokens != 4096 || cfg.CoderMaxTokens != 4096 || cfg.ReviewerMaxTokens != 1536 || cfg.TesterMaxTokens != 1536 || cfg.FixerMaxTokens != 4096 || cfg.LLMReasoningEffort != "none" || cfg.LLMTemperature != 0.2 || cfg.LogLevel != "info" || cfg.CoderBackend != "direct" || cfg.PiExecutable != "pi" || cfg.PiTimeout != 300*time.Second {
		t.Fatalf("Load() defaults = %+v", cfg)
	}
}

func TestLoadLLMOverrides(t *testing.T) {
	t.Setenv("MAC_REPOSITORY_ROOT", t.TempDir())
	t.Setenv("MAC_LLM_TIMEOUT", "45s")
	t.Setenv("MAC_LLM_MAX_TOKENS", "512")
	t.Setenv("MAC_CODER_MAX_TOKENS", "8192")
	t.Setenv("MAC_REVIEWER_MAX_TOKENS", "4096")
	t.Setenv("MAC_TESTER_MAX_TOKENS", "3072")
	t.Setenv("MAC_FIXER_MAX_TOKENS", "8192")
	t.Setenv("MAC_LLM_REASONING_EFFORT", "low")
	t.Setenv("MAC_LLM_TEMPERATURE", "0.7")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLMTimeout != 45*time.Second || cfg.LLMMaxTokens != 512 || cfg.CoderMaxTokens != 8192 || cfg.ReviewerMaxTokens != 4096 || cfg.TesterMaxTokens != 3072 || cfg.FixerMaxTokens != 8192 || cfg.LLMReasoningEffort != "low" || cfg.LLMTemperature != 0.7 {
		t.Fatalf("Load() overrides = %+v", cfg)
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
		{name: "max tokens syntax", env: "MAC_LLM_MAX_TOKENS", value: "many"},
		{name: "max tokens range", env: "MAC_LLM_MAX_TOKENS", value: "0"},
		{name: "coder max tokens syntax", env: "MAC_CODER_MAX_TOKENS", value: "many"},
		{name: "coder max tokens range", env: "MAC_CODER_MAX_TOKENS", value: "0"},
		{name: "reviewer max tokens range", env: "MAC_REVIEWER_MAX_TOKENS", value: "0"},
		{name: "tester max tokens range", env: "MAC_TESTER_MAX_TOKENS", value: "0"},
		{name: "fixer max tokens range", env: "MAC_FIXER_MAX_TOKENS", value: "0"},
		{name: "temperature syntax", env: "MAC_LLM_TEMPERATURE", value: "warm"},
		{name: "temperature range", env: "MAC_LLM_TEMPERATURE", value: "2.1"},
		{name: "log level", env: "MAC_LOG_LEVEL", value: "verbose"},
		{name: "coder backend", env: "MAC_CODER_BACKEND", value: "magic"},
		{name: "pi timeout", env: "MAC_PI_TIMEOUT", value: "0s"},
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

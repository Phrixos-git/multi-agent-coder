package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("MAC_CODER_BACKEND", "")
	t.Setenv("MAC_PI_TIMEOUT", "")
	t.Setenv("MAC_LLM_BASE_URL", "")
	t.Setenv("MAC_LLM_MODEL", "")
	t.Setenv("MAC_LLM_TIMEOUT", "")
	t.Setenv("MAC_CODING_LLM_BASE_URL", "")
	t.Setenv("MAC_CODING_LLM_MODEL", "")
	t.Setenv("MAC_CODING_LLM_TIMEOUT", "")
	t.Setenv("MAC_LLM_MAX_TOKENS", "")
	t.Setenv("MAC_CODER_MAX_TOKENS", "")
	t.Setenv("MAC_REVIEWER_MAX_TOKENS", "")
	t.Setenv("MAC_TESTER_MAX_TOKENS", "")
	t.Setenv("MAC_FIXER_MAX_TOKENS", "")
	t.Setenv("MAC_LLM_REASONING_EFFORT", "")
	t.Setenv("MAC_LLM_TEMPERATURE", "")
	t.Setenv("MAC_REPOSITORY_ROOT", t.TempDir())
	t.Setenv("MAC_LOG_LEVEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MainLLM != (LLMConfig{BaseURL: defaultLLMBaseURL, Model: defaultLLMModel, Timeout: 180 * time.Second}) || cfg.CodingLLM != (LLMConfig{BaseURL: defaultCodingLLMBaseURL, Model: defaultCodingLLMModel, Timeout: 180 * time.Second}) || cfg.LLMMaxTokens != 4096 || cfg.CoderMaxTokens != 4096 || cfg.ReviewerMaxTokens != 4096 || cfg.TesterMaxTokens != 3072 || cfg.FixerMaxTokens != 4096 || cfg.LLMReasoningEffort != "none" || cfg.LLMTemperature != 0.2 || cfg.LogLevel != "info" {
		t.Fatalf("Load() defaults = %+v", cfg)
	}
	if cfg.CoderBackend != "direct" || cfg.FixerBackend != "direct" || cfg.PiExecutable != "pi" || cfg.PiProvider != "local-qwen" || cfg.PiModel != "qwen" || cfg.PiTimeout != 0 {
		t.Fatalf("Pi defaults = %+v", cfg)
	}
}

func TestLoadPiBackendAndUnlimitedTimeout(t *testing.T) {
	t.Setenv("MAC_REPOSITORY_ROOT", t.TempDir())
	t.Setenv("MAC_CODER_BACKEND", "pi")
	t.Setenv("MAC_PI_TIMEOUT", "0")
	t.Setenv("MAC_PI_EXECUTABLE", "/tmp/pi")
	t.Setenv("MAC_PI_PROVIDER", "local-qwen")
	t.Setenv("MAC_PI_MODEL", "qwen")
	t.Setenv("MAC_PI_AGENT_DIR", "/tmp/agent")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CoderBackend != "pi" || cfg.PiTimeout != 0 || cfg.PiExecutable != "/tmp/pi" || cfg.PiAgentDir != "/tmp/agent" {
		t.Fatalf("Pi config = %+v", cfg)
	}
}

func TestLoadCoderAndFixerBackendsAreIndependent(t *testing.T) {
	for _, pair := range []struct{ coder, fixer string }{{"direct", "direct"}, {"pi", "direct"}, {"direct", "pi"}, {"pi", "pi"}} {
		t.Run(pair.coder+"-"+pair.fixer, func(t *testing.T) {
			t.Setenv("MAC_REPOSITORY_ROOT", t.TempDir())
			t.Setenv("MAC_CODER_BACKEND", pair.coder)
			t.Setenv("MAC_FIXER_BACKEND", pair.fixer)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CoderBackend != pair.coder || cfg.FixerBackend != pair.fixer {
				t.Fatalf("backends = %s/%s", cfg.CoderBackend, cfg.FixerBackend)
			}
		})
	}
	t.Setenv("MAC_REPOSITORY_ROOT", t.TempDir())
	t.Setenv("MAC_FIXER_BACKEND", "invalid")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MAC_FIXER_BACKEND") {
		t.Fatalf("invalid fixer backend error = %v", err)
	}
}

func TestLoadRejectsInvalidPiBackend(t *testing.T) {
	t.Setenv("MAC_REPOSITORY_ROOT", t.TempDir())
	t.Setenv("MAC_CODER_BACKEND", "unknown")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted invalid coder backend")
	}
}

func TestLoadRejectsPiRPCArgsThatOverrideRestrictedTools(t *testing.T) {
	t.Setenv("MAC_REPOSITORY_ROOT", t.TempDir())
	t.Setenv("MAC_PI_RPC_ARGS", "--tools read,write,grep")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted Pi tool override")
	}
}

func TestLoadLLMOverrides(t *testing.T) {
	t.Setenv("MAC_REPOSITORY_ROOT", t.TempDir())
	t.Setenv("MAC_LLM_TIMEOUT", "45s")
	t.Setenv("MAC_LLM_BASE_URL", "http://127.0.0.1:19100")
	t.Setenv("MAC_LLM_MODEL", "main-test")
	t.Setenv("MAC_CODING_LLM_BASE_URL", "http://127.0.0.1:19101")
	t.Setenv("MAC_CODING_LLM_MODEL", "coding-test")
	t.Setenv("MAC_CODING_LLM_TIMEOUT", "55s")
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
	if cfg.MainLLM != (LLMConfig{BaseURL: "http://127.0.0.1:19100", Model: "main-test", Timeout: 45 * time.Second}) || cfg.CodingLLM != (LLMConfig{BaseURL: "http://127.0.0.1:19101", Model: "coding-test", Timeout: 55 * time.Second}) || cfg.LLMMaxTokens != 512 || cfg.CoderMaxTokens != 8192 || cfg.ReviewerMaxTokens != 4096 || cfg.TesterMaxTokens != 3072 || cfg.FixerMaxTokens != 8192 || cfg.LLMReasoningEffort != "low" || cfg.LLMTemperature != 0.7 {
		t.Fatalf("Load() overrides = %+v", cfg)
	}
}

func TestLoadRoleTokenPrecedence(t *testing.T) {
	t.Setenv("MAC_REPOSITORY_ROOT", t.TempDir())
	t.Setenv("MAC_LLM_MAX_TOKENS", "2048")
	for _, name := range []string{"MAC_CODER_MAX_TOKENS", "MAC_REVIEWER_MAX_TOKENS", "MAC_TESTER_MAX_TOKENS", "MAC_FIXER_MAX_TOKENS"} {
		t.Setenv(name, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CoderMaxTokens != 2048 || cfg.ReviewerMaxTokens != 2048 || cfg.TesterMaxTokens != 2048 || cfg.FixerMaxTokens != 2048 {
		t.Fatalf("global fallback = %+v", cfg)
	}
	t.Setenv("MAC_TESTER_MAX_TOKENS", "3072")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TesterMaxTokens != 3072 || cfg.ReviewerMaxTokens != 2048 {
		t.Fatalf("role override = %+v", cfg)
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
		{name: "coding URL", env: "MAC_CODING_LLM_BASE_URL", value: "localhost:18082"},
		{name: "coding timeout", env: "MAC_CODING_LLM_TIMEOUT", value: "0s"},
		{name: "negative Pi timeout", env: "MAC_PI_TIMEOUT", value: "-1s"},
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

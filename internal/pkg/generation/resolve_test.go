package generation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type resolveSearch struct{}

func (resolveSearch) Search(context.Context, string, int) ([]SearchResult, error) {
	return nil, nil
}

func boolPtr(v bool) *bool { return &v }

func TestResolveAndNewProvider(t *testing.T) {
	t.Run("empty provider", func(t *testing.T) {
		cfg, err := Resolve(ProviderSettings{APIKey: "sk"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.OpenAIBaseURL != "https://api.openai.com/v1" || cfg.Local || cfg.OpenAIAPIKey != "sk" {
			t.Fatalf("cfg = %+v", cfg)
		}
	})

	t.Run("ollama and local", func(t *testing.T) {
		for _, name := range []string{" ollama ", "local"} {
			cfg, err := Resolve(ProviderSettings{Provider: name})
			if err != nil {
				t.Fatal(err)
			}
			if cfg.OpenAIAPIKey != "local" || cfg.OpenAIBaseURL != "http://localhost:11434/v1" || !cfg.Local {
				t.Fatalf("%s cfg = %+v", name, cfg)
			}
			if cfg.OpenAIModelTrial != "llama3.1" || cfg.OpenAIModelPaid != "llama3.1" {
				t.Fatalf("%s models = %s %s", name, cfg.OpenAIModelTrial, cfg.OpenAIModelPaid)
			}
			p, err := NewProvider(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if p.Name() != "openai" || !p.IsLocal() || p.ModelForTier(false) != "llama3.1" || p.ModelForTier(true) != "llama3.1" {
				t.Fatalf("provider name=%s local=%t trial=%s paid=%s", p.Name(), p.IsLocal(), p.ModelForTier(false), p.ModelForTier(true))
			}
		}
	})

	t.Run("ollama free false", func(t *testing.T) {
		cfg, err := Resolve(ProviderSettings{Provider: "ollama", Free: boolPtr(false)})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Local || cfg.OpenAIAPIKey != "" {
			t.Fatalf("cfg = %+v", cfg)
		}
		_, err = NewProvider(cfg)
		if !errors.Is(err, ErrProviderNotConfigured) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("ollama base override", func(t *testing.T) {
		cfg, err := Resolve(ProviderSettings{Provider: "ollama", APIKey: "secret", BaseURL: "http://example.test/v1"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.OpenAIBaseURL != "http://example.test/v1" || cfg.OpenAIAPIKey != "secret" || !cfg.Local {
			t.Fatalf("cfg = %+v", cfg)
		}
	})

	t.Run("model precedence", func(t *testing.T) {
		cfg, err := Resolve(ProviderSettings{Provider: "ollama", APIKey: "k", Model: "m", ModelTrial: "t", ModelPaid: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.OpenAIModelTrial != "t" || cfg.OpenAIModelPaid != "p" {
			t.Fatalf("models = %s %s", cfg.OpenAIModelTrial, cfg.OpenAIModelPaid)
		}
		cfg, err = Resolve(ProviderSettings{Provider: "ollama", APIKey: "k", Model: "m", ModelTrial: "t"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.OpenAIModelPaid != "m" {
			t.Fatalf("paid = %s", cfg.OpenAIModelPaid)
		}
	})

	t.Run("openrouter", func(t *testing.T) {
		cfg, err := Resolve(ProviderSettings{Provider: "openrouter", APIKey: "k"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Local || cfg.OpenAIModelTrial != "meta-llama/llama-3.1-8b-instruct:free" || cfg.OpenAIModelPaid != "meta-llama/llama-3.1-8b-instruct:free" {
			t.Fatalf("cfg = %+v", cfg)
		}
	})

	t.Run("groq", func(t *testing.T) {
		cfg, err := Resolve(ProviderSettings{Provider: "groq", APIKey: "k"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Local || cfg.OpenAIBaseURL != "https://api.groq.com/openai/v1" || cfg.OpenAIModelTrial != "openai/gpt-oss-20b" {
			t.Fatalf("cfg = %+v", cfg)
		}
	})

	t.Run("openai explicit free", func(t *testing.T) {
		cfg, err := Resolve(ProviderSettings{Provider: "openai", APIKey: "sk", Free: boolPtr(true)})
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Local || cfg.OpenAIBaseURL != "https://api.openai.com/v1" || cfg.OpenAIAPIKey != "sk" {
			t.Fatalf("cfg = %+v", cfg)
		}
	})

	t.Run("openai empty key", func(t *testing.T) {
		cfg, err := Resolve(ProviderSettings{Provider: "openai"})
		if err != nil || cfg.OpenAIAPIKey != "" || cfg.OpenAIBaseURL != "https://api.openai.com/v1" {
			t.Fatalf("cfg = %+v err=%v", cfg, err)
		}
		_, err = NewProvider(cfg)
		if !errors.Is(err, ErrProviderNotConfigured) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("anthropic ignores openai settings", func(t *testing.T) {
		search := SearchClient(&resolveSearch{})
		cfg, err := Resolve(ProviderSettings{
			Provider: "ANTHROPIC", APIKey: "ant", BaseURL: "http://nope", Model: "nope-model", Free: boolPtr(true), Search: search,
		})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.AnthropicAPIKey != "ant" || cfg.Search != search || cfg.OpenAIAPIKey != "" || cfg.OpenAIBaseURL != "" || cfg.OpenAIModelTrial != "" || cfg.OpenAIModelPaid != "" || cfg.Local {
			t.Fatalf("cfg = %+v", cfg)
		}
		p, err := NewProvider(cfg)
		if err != nil || p.Name() != "anthropic" || p.IsLocal() {
			t.Fatalf("provider=%v err=%v", p, err)
		}
	})

	t.Run("anthropic empty key", func(t *testing.T) {
		cfg, err := Resolve(ProviderSettings{Provider: "anthropic"})
		if err != nil || cfg.AnthropicAPIKey != "" {
			t.Fatalf("cfg = %+v err=%v", cfg, err)
		}
		_, err = NewProvider(cfg)
		if !errors.Is(err, ErrProviderNotConfigured) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("custom needs base", func(t *testing.T) {
		for _, name := range []string{"custom", "openai-compatible", "vllm"} {
			_, err := Resolve(ProviderSettings{Provider: name, APIKey: "k"})
			if err == nil || !strings.Contains(err.Error(), `"`+name+`"`) {
				t.Fatalf("%s err = %v", name, err)
			}
		}
	})

	t.Run("unknown with base", func(t *testing.T) {
		cfg, err := Resolve(ProviderSettings{Provider: "vllm", APIKey: "k", BaseURL: "http://vllm.test/v1"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Local || cfg.OpenAIBaseURL != "http://vllm.test/v1" || cfg.OpenAIAPIKey != "k" || cfg.AnthropicAPIKey != "" {
			t.Fatalf("cfg = %+v", cfg)
		}
	})

	t.Run("openai key wins", func(t *testing.T) {
		p, err := NewProvider(ProviderConfig{OpenAIAPIKey: "o", AnthropicAPIKey: "a"})
		if err != nil || p.Name() != "openai" {
			t.Fatalf("provider=%v err=%v", p, err)
		}
	})

	t.Run("no key", func(t *testing.T) {
		_, err := NewProvider(ProviderConfig{})
		if !errors.Is(err, ErrProviderNotConfigured) {
			t.Fatalf("err = %v", err)
		}
	})
}

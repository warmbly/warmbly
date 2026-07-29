package generation

import (
	"fmt"
	"strings"
)

// This file makes provider selection a single choice. A self-hoster sets
// AI_PROVIDER to a preset (openrouter, groq, ollama, openai, anthropic, custom)
// and supplies just a key + model; the preset fills in the base URL and the
// free/un-metered default. OpenRouter is the OSS-agentic-tool pattern: one
// OpenAI-compatible endpoint + one key fronting every vendor's models, so
// "switching models" is just changing AI_MODEL.

// ProviderSettings are the raw, env-sourced inputs for provider selection.
type ProviderSettings struct {
	// Provider is the AI_PROVIDER preset. Empty means openai.
	Provider string
	// APIKey is the key for the selected provider (AI_API_KEY).
	APIKey string
	// BaseURL overrides the preset endpoint (AI_BASE_URL).
	BaseURL string
	// Model sets both tiers (AI_MODEL). ModelTrial / ModelPaid override per tier.
	Model      string
	ModelTrial string
	ModelPaid  string
	// Free marks the model as un-metered (AI_FREE). nil uses the preset default
	// (true for local backends like Ollama).
	Free   *bool
	Search SearchClient
}

type providerPreset struct {
	baseURL      string
	defaultModel string
	free         bool
	anthropic    bool
	// needsKey is false for local backends that ignore the bearer token.
	needsKey bool
}

// presetFor returns the built-in preset for a provider name. The second result
// is false for an unknown name (treated as a custom OpenAI-compatible endpoint).
func presetFor(name string) (providerPreset, bool) {
	switch name {
	case "openai":
		// Model default handled by newOpenAIProvider (gpt-4o-mini / gpt-4o).
		return providerPreset{baseURL: defaultOpenAIBaseURL, needsKey: true}, true
	case "openrouter":
		return providerPreset{baseURL: "https://openrouter.ai/api/v1", defaultModel: "meta-llama/llama-3.1-8b-instruct:free", needsKey: true}, true
	case "groq":
		return providerPreset{baseURL: "https://api.groq.com/openai/v1", defaultModel: "openai/gpt-oss-20b", needsKey: true}, true
	case "ollama", "local":
		return providerPreset{baseURL: "http://localhost:11434/v1", defaultModel: defaultLocalModel, free: true}, true
	case "anthropic":
		return providerPreset{anthropic: true, needsKey: true}, true
	case "custom", "openai-compatible":
		// Base URL must be supplied via AI_BASE_URL.
		return providerPreset{needsKey: true}, true
	}
	return providerPreset{needsKey: true}, false
}

// Resolve maps settings + the chosen preset into a ProviderConfig for
// NewProvider. Explicit BaseURL / Free / model values always win over the preset.
// It errors for a custom/openai-compatible provider with no base URL rather than
// silently defaulting to OpenAI's public API (which would send the operator's key
// to the wrong host).
func Resolve(s ProviderSettings) (ProviderConfig, error) {
	name := strings.ToLower(strings.TrimSpace(s.Provider))
	if name == "" {
		name = "openai"
	}
	preset, _ := presetFor(name)

	if preset.anthropic {
		return ProviderConfig{AnthropicAPIKey: s.APIKey, Search: s.Search}, nil
	}

	free := preset.free
	if s.Free != nil {
		free = *s.Free
	}
	apiKey := s.APIKey
	if strings.TrimSpace(apiKey) == "" && free {
		// Local backends ignore the bearer token, but NewProvider needs a
		// non-empty key to select the OpenAI-compatible provider.
		apiKey = "local"
	}
	baseURL := strings.TrimSpace(s.BaseURL)
	if baseURL == "" {
		baseURL = preset.baseURL
	}
	// Only a custom / openai-compatible (or unknown) provider reaches here with an
	// empty base; every built-in preset sets one. Refuse rather than falling
	// through to api.openai.com with a non-OpenAI key.
	if baseURL == "" {
		return ProviderConfig{}, fmt.Errorf("AI_PROVIDER=%q needs AI_BASE_URL: a custom OpenAI-compatible provider has no default endpoint", name)
	}
	return ProviderConfig{
		OpenAIAPIKey:     apiKey,
		OpenAIBaseURL:    baseURL,
		OpenAIModelTrial: firstNonEmpty(s.ModelTrial, s.Model, preset.defaultModel),
		OpenAIModelPaid:  firstNonEmpty(s.ModelPaid, s.Model, preset.defaultModel),
		Local:            free,
		Search:           s.Search,
	}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

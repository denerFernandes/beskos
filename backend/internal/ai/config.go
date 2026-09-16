package ai

import (
	"fmt"
	"os"
)

// Config selects and configures the provider per environment. Adding a new provider in the
// future = adding a case in New() + its Provider implementation. Nothing else changes.
type Config struct {
	Provider string // "deepseek" (default). Future: "claude", "openai", ...
	APIKey   string
	BaseURL  string // optional; each provider has a default
	Model    string // optional; each provider has a default
}

// FromEnv reads the config from the environment (secrets outside the code — law §10).
//
//	AI_PROVIDER   (default "deepseek")
//	AI_API_KEY    (required)
//	AI_BASE_URL   (optional)
//	AI_MODEL      (optional; e.g.: "deepseek-v4")
func FromEnv() Config {
	return Config{
		Provider: getenv("AI_PROVIDER", "deepseek"),
		APIKey:   os.Getenv("AI_API_KEY"),
		BaseURL:  os.Getenv("AI_BASE_URL"),
		Model:    os.Getenv("AI_MODEL"),
	}
}

// New builds the Provider from the config. It's the ONLY place that knows concrete providers.
func New(cfg Config) (Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("ai: AI_API_KEY is required")
	}
	switch cfg.Provider {
	case "deepseek", "":
		return NewDeepSeek(cfg.APIKey, cfg.BaseURL, cfg.Model), nil
	case "tensorx":
		// Same protocol (OpenAI-compatible), different destination: TensorX serves open models
		// on European infrastructure. It's the difference between the customer's content staying
		// in the EU or ending up with the model's creator — and it's what backs the sentence on
		// the subprocessors page.
		base := cfg.BaseURL
		if base == "" {
			base = "https://api.tensorx.ai/v1"
		}
		model := cfg.Model
		if model == "" {
			model = "deepseek/deepseek-v4-flash"
		}
		c := NewDeepSeek(cfg.APIKey, base, model)
		c.name = "tensorx"
		return c, nil
	// case "claude": return NewClaude(...), nil   // futuro
	// case "openai": return NewOpenAI(...), nil   // futuro
	default:
		return nil, fmt.Errorf("ai: unknown provider %q", cfg.Provider)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

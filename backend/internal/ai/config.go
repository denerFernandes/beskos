package ai

import (
	"fmt"
	"os"
)

// Config seleciona e configura o provider por ambiente. Adicionar um provider novo no futuro
// = adicionar um case em New() + sua implementação de Provider. Nada mais muda.
type Config struct {
	Provider string // "deepseek" (default). Futuro: "claude", "openai", ...
	APIKey   string
	BaseURL  string // opcional; cada provider tem um default
	Model    string // opcional; cada provider tem um default
}

// FromEnv lê a config do ambiente (segredos fora do código — lei §10).
//
//	AI_PROVIDER   (default "deepseek")
//	AI_API_KEY    (obrigatório)
//	AI_BASE_URL   (opcional)
//	AI_MODEL      (opcional; ex.: "deepseek-v4")
func FromEnv() Config {
	return Config{
		Provider: getenv("AI_PROVIDER", "deepseek"),
		APIKey:   os.Getenv("AI_API_KEY"),
		BaseURL:  os.Getenv("AI_BASE_URL"),
		Model:    os.Getenv("AI_MODEL"),
	}
}

// New constrói o Provider a partir da config. É o ÚNICO ponto que conhece providers concretos.
func New(cfg Config) (Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("ai: AI_API_KEY is required")
	}
	switch cfg.Provider {
	case "deepseek", "":
		return NewDeepSeek(cfg.APIKey, cfg.BaseURL, cfg.Model), nil
	case "tensorx":
		// Mesmo protocolo (compatível com OpenAI), outro destino: a TensorX serve modelos abertos
		// em infraestrutura europeia. É a diferença entre o conteúdo do cliente ficar na UE ou ir
		// parar ao criador do modelo — e é o que sustenta a frase da página de subprocessadores.
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

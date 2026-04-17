package config

import "os"

type Config struct {
	OpenAIAPIKey  string
	OpenAIBaseURL string
	OpenAIModel   string
}

func Load() Config {
	cfg := Config{
		OpenAIAPIKey:  os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL: os.Getenv("OPENAI_BASE_URL"),
		OpenAIModel:   os.Getenv("OPENAI_MODEL"),
	}
	if cfg.OpenAIBaseURL == "" {
		cfg.OpenAIBaseURL = "https://api.openai.com"
	}
	if cfg.OpenAIModel == "" {
		cfg.OpenAIModel = "gpt-4o-mini"
	}
	return cfg
}

package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramBotToken    string
	OutlineAPIKey       string
	OutlineBaseURL      string
	OutlineCollectionID string
	LLMAPIKey           string
	LLMBaseURL          string
	LLMModel            string
	ScheduleDocID       string
}

func Load() (*Config, error) {
	_ = godotenv.Load() // Ignore error as .env is optional

	cfg := &Config{
		TelegramBotToken:    os.Getenv("TELEGRAM_BOT_TOKEN"),
		OutlineAPIKey:       os.Getenv("OUTLINE_API_KEY"),
		OutlineBaseURL:      os.Getenv("OUTLINE_BASE_URL"),
		OutlineCollectionID: os.Getenv("OUTLINE_COLLECTION_ID"),
		LLMAPIKey:           os.Getenv("LLM_API_KEY"),
		LLMBaseURL:          os.Getenv("LLM_BASE_URL"),
		LLMModel:            os.Getenv("LLM_MODEL"),
		ScheduleDocID:       os.Getenv("SCHEDULE_DOC_ID"),
	}

	if cfg.TelegramBotToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	if cfg.LLMModel == "" {
		cfg.LLMModel = "gpt-4o"
	}

	if cfg.LLMBaseURL == "" {
		cfg.LLMBaseURL = "https://api.openai.com/v1"
	}

	return cfg, nil
}

package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramBotToken    string
	TelegramAdminChatID int64
	OutlineAPIKey       string
	OutlineBaseURL      string
	OutlineCollectionID string
	LLMAPIKey           string
	LLMBaseURL          string
	LLMModel            string
	ScheduleDocID       string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

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

	required := map[string]string{
		"TELEGRAM_BOT_TOKEN":    cfg.TelegramBotToken,
		"OUTLINE_API_KEY":       cfg.OutlineAPIKey,
		"OUTLINE_BASE_URL":      cfg.OutlineBaseURL,
		"OUTLINE_COLLECTION_ID": cfg.OutlineCollectionID,
		"LLM_API_KEY":           cfg.LLMAPIKey,
		"SCHEDULE_DOC_ID":       cfg.ScheduleDocID,
	}
	for name, value := range required {
		if value == "" {
			return nil, fmt.Errorf("%s is required", name)
		}
	}

	adminRaw := os.Getenv("TELEGRAM_ADMIN_CHAT_ID")
	if adminRaw == "" {
		return nil, fmt.Errorf("TELEGRAM_ADMIN_CHAT_ID is required")
	}
	adminID, err := strconv.ParseInt(adminRaw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("TELEGRAM_ADMIN_CHAT_ID must be an integer: %w", err)
	}
	cfg.TelegramAdminChatID = adminID

	if cfg.LLMModel == "" {
		cfg.LLMModel = "gpt-4o"
	}

	if cfg.LLMBaseURL == "" {
		cfg.LLMBaseURL = "https://api.openai.com/v1"
	}

	return cfg, nil
}

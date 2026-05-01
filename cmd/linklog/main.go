package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/korjavin/linklog/internal/bot"
	"github.com/korjavin/linklog/internal/config"
	"github.com/korjavin/linklog/internal/llm"
	"github.com/korjavin/linklog/internal/mcp"
	"github.com/korjavin/linklog/internal/outline"
	"github.com/korjavin/linklog/internal/scheduler"
	"github.com/sashabaranov/go-openai"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting LinkLog Bot (model=%s, outline=%s)", cfg.LLMModel, cfg.OutlineBaseURL)

	ctx := context.Background()

	outClient := outline.NewClient(cfg.OutlineAPIKey, cfg.OutlineBaseURL)
	mcpEnv := []string{
		"OUTLINE_API_KEY=" + cfg.OutlineAPIKey,
		"OUTLINE_BASE_URL=" + cfg.OutlineBaseURL,
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
	}
	mcpClient, err := mcp.NewClient(ctx, "npx", []string{"-y", "@spicesh/mcp-outline"}, mcpEnv)
	if err != nil {
		log.Fatalf("Failed to initialize MCP client: %v", err)
	}
	defer func() { _ = mcpClient.Close() }()

	openaiConfig := openai.DefaultConfig(cfg.LLMAPIKey)
	openaiConfig.BaseURL = cfg.LLMBaseURL
	openaiClient := openai.NewClientWithConfig(openaiConfig)

	llmService := llm.NewService(openaiClient, mcpClient, cfg.OutlineCollectionID, cfg.LLMModel)

	tgBot, err := bot.NewBot(cfg.TelegramBotToken, cfg.TelegramAdminChatID, llmService, outClient, cfg.ScheduleDocID)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	sched := scheduler.NewScheduler(outClient, tgBot, cfg.ScheduleDocID)
	if err := sched.Start(); err != nil {
		log.Fatalf("Failed to start scheduler: %v", err)
	}

	go tgBot.Start()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down...")
	sched.Stop()
	tgBot.Stop()
}

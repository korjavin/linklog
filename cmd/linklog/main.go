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

	log.Printf("Starting LinkLog Bot...")
	log.Printf("LLM Model: %s", cfg.LLMModel)
	log.Printf("Outline Base URL: %s", cfg.OutlineBaseURL)

	ctx := context.Background()
	env := os.Environ()
	// Optionally add explicit env vars if they are not already in os.Environ() (which they are from godotenv)
	env = append(env, "OUTLINE_API_KEY="+cfg.OutlineAPIKey)
	env = append(env, "OUTLINE_BASE_URL="+cfg.OutlineBaseURL)

	// Initialize dependencies
	outClient := outline.NewClient(cfg.OutlineAPIKey, cfg.OutlineBaseURL)
	mcpClient, err := mcp.NewClient(ctx, "npx", []string{"-y", "@spicesh/mcp-outline"}, env)
	if err != nil {
		log.Fatalf("Failed to initialize MCP client: %v", err)
	}
	defer func() {
		_ = mcpClient.Close()
	}()
	
	openaiConfig := openai.DefaultConfig(cfg.LLMAPIKey)
	openaiConfig.BaseURL = cfg.LLMBaseURL
	openaiClient := openai.NewClientWithConfig(openaiConfig)
	
	llmService := llm.NewService(openaiClient, mcpClient, cfg.OutlineCollectionID, cfg.LLMModel)

	// Initialize Bot
	tgBot, err := bot.NewBot(cfg.TelegramBotToken, llmService, outClient, cfg.ScheduleDocID)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	// Initialize and start Scheduler
	sched := scheduler.NewScheduler(outClient, tgBot, cfg.ScheduleDocID)
	sched.Start()

	// Start bot in background
	go tgBot.Start()

	// Wait for termination signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down...")
	sched.Stop()
	tgBot.Stop()
	log.Println("Done.")
}

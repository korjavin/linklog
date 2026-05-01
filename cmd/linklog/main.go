package main

import (
	"log"

	"github.com/korjavin/linklog/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting LinkLog Bot...")
	log.Printf("LLM Model: %s", cfg.LLMModel)
	log.Printf("Outline Base URL: %s", cfg.OutlineBaseURL)
	
	// TODO: Initialize bot, mcp, etc.
}

package llm

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/korjavin/linklog/internal/mcp"
	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMServiceIntegration(t *testing.T) {
	_ = godotenv.Load("../../.env") // Load .env if present

	apiKey := os.Getenv("LLM_API_KEY")
	baseURL := os.Getenv("LLM_BASE_URL")
	model := os.Getenv("LLM_MODEL")
	outlineKey := os.Getenv("OUTLINE_API_KEY")
	outlineURL := os.Getenv("OUTLINE_BASE_URL")

	if apiKey == "" || outlineKey == "" || outlineURL == "" {
		t.Skip("Skipping LLM Service integration test: required environment variables not set")
	}

	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = openai.GPT4o
	}

	env := os.Environ()
	env = append(env, "OUTLINE_API_KEY="+outlineKey)
	env = append(env, "OUTLINE_BASE_URL="+outlineURL)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mcpClient, err := mcp.NewClient(ctx, "npx", []string{"-y", "@spicesh/mcp-outline"}, env)
	require.NoError(t, err)
	defer mcpClient.Close()

	openaiConfig := openai.DefaultConfig(apiKey)
	openaiConfig.BaseURL = baseURL
	openaiClient := openai.NewClientWithConfig(openaiConfig)

	collectionID := os.Getenv("OUTLINE_COLLECTION_ID")

	svc := NewService(openaiClient, mcpClient, collectionID, model)

	// Since we are running a live test, we'll ask the model a simple question that might use Outline
	// or at least test the interaction loop.
	reply, nextDate, err := svc.ProcessInteraction(ctx, "Hello! Please list the collections in Outline. Do not create anything.")
	require.NoError(t, err)

	assert.NotEmpty(t, reply)
	assert.NotEmpty(t, nextDate)

	t.Logf("LLM Reply: %s", reply)
	t.Logf("Suggested Next Date: %s", nextDate)
}

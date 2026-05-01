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

func TestParseFollowUpJSON(t *testing.T) {
	fu := parseFollowUp(`{"contact":"Alice","date":"2026-06-01"}`, "2026-12-31")
	assert.Equal(t, "Alice", fu.Contact)
	assert.Equal(t, "2026-06-01", fu.Date)
}

func TestParseFollowUpFencedJSON(t *testing.T) {
	fu := parseFollowUp("```json\n{\"contact\":\"Bob\",\"date\":\"2026-07-01\"}\n```", "2026-12-31")
	assert.Equal(t, "Bob", fu.Contact)
	assert.Equal(t, "2026-07-01", fu.Date)
}

func TestParseFollowUpNone(t *testing.T) {
	fu := parseFollowUp(`{"contact":"Carol","date":"none"}`, "2026-12-31")
	assert.Equal(t, "Carol", fu.Contact)
	assert.Equal(t, "", fu.Date)
}

func TestParseFollowUpInvalidDateFallsBack(t *testing.T) {
	fu := parseFollowUp(`{"contact":"Dan","date":"next Friday"}`, "2026-12-31")
	assert.Equal(t, "Dan", fu.Contact)
	assert.Equal(t, "2026-12-31", fu.Date)
}

func TestParseFollowUpExtractsDateFromProse(t *testing.T) {
	future := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	fu := parseFollowUp("Sure, let's say "+future+".", "2026-12-31")
	assert.Equal(t, future, fu.Date)
}

func TestParseFollowUpIgnoresPastDateInProse(t *testing.T) {
	// A past date mentioned in prose (e.g., "last met on 2020-01-15") should not
	// be picked up as the next-contact date — fall back to the default instead.
	fu := parseFollowUp("We last met on 2020-01-15, see you again soon.", "2026-12-31")
	assert.Equal(t, "2026-12-31", fu.Date)
}

func TestLLMServiceIntegration(t *testing.T) {
	_ = godotenv.Load("../../.env")

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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mcpClient, err := mcp.NewClient(ctx, "npx", []string{"-y", "@spicesh/mcp-outline"}, os.Environ())
	require.NoError(t, err)
	defer func() { _ = mcpClient.Close() }()

	openaiConfig := openai.DefaultConfig(apiKey)
	openaiConfig.BaseURL = baseURL
	openaiClient := openai.NewClientWithConfig(openaiConfig)

	collectionID := os.Getenv("OUTLINE_COLLECTION_ID")

	svc := NewService(openaiClient, mcpClient, collectionID, model)

	reply, followUp, err := svc.ProcessInteraction(ctx, "Hello! Please list the collections in Outline. Do not create anything.")
	require.NoError(t, err)

	assert.NotEmpty(t, reply)
	t.Logf("LLM Reply: %s", reply)
	t.Logf("Follow-up: %+v", followUp)
}

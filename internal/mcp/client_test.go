package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolToOpenAIFunction(t *testing.T) {
	tool := mcp.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"param1": map[string]interface{}{
					"type": "string",
				},
			},
		},
	}

	fn := ToolToOpenAIFunction(tool)

	assert.Equal(t, "test_tool", fn.Name)
	assert.Equal(t, "A test tool", fn.Description)

	params, ok := fn.Parameters.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "object", params["type"])
}

func TestOutlineMCPIntegration(t *testing.T) {
	_ = godotenv.Load("../../.env") // Load .env if present

	apiKey := os.Getenv("OUTLINE_API_KEY")
	baseURL := os.Getenv("OUTLINE_BASE_URL")

	if apiKey == "" || baseURL == "" {
		t.Skip("Skipping Outline MCP integration test: OUTLINE_API_KEY or OUTLINE_BASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := NewHTTPClient(ctx, baseURL, apiKey)
	require.NoError(t, err)
	defer func() {
		_ = client.Close()
	}()

	tools, err := client.ListTools(ctx)
	require.NoError(t, err)

	// We expect the outline server to have some tools
	assert.NotEmpty(t, tools)

	t.Logf("Found %d tools from Outline MCP", len(tools))
	for _, tool := range tools {
		t.Logf("Tool: %s - %s", tool.Name, tool.Description)
	}
}

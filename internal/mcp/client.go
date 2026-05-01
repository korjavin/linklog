package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sashabaranov/go-openai"
)

type Client struct {
	mcpClient mcplib.MCPClient
	cancel    context.CancelFunc
}

// NewHTTPClient connects to an Outline MCP server over HTTP (SSE or streamable-HTTP).
// baseURL should be the Outline base URL (e.g. https://outline.example.com); the
// /api/mcp path is appended automatically. apiKey is sent as a Bearer token.
func NewHTTPClient(ctx context.Context, baseURL, apiKey string) (*Client, error) {
	ctx, cancel := context.WithCancel(ctx)

	endpoint := strings.TrimSuffix(baseURL, "/") + "/mcp"
	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
	}

	mcpClient, err := mcplib.NewStreamableHttpClient(endpoint, transport.WithHTTPHeaders(headers))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create MCP client: %w", err)
	}

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "linklog-bot",
		Version: "1.0.0",
	}

	res, err := mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		_ = mcpClient.Close()
		cancel()
		return nil, fmt.Errorf("failed to initialize MCP client: %w", err)
	}

	if res.ProtocolVersion == "" {
		_ = mcpClient.Close()
		cancel()
		return nil, fmt.Errorf("initialization failed, no protocol version returned")
	}

	return &Client{
		mcpClient: mcpClient,
		cancel:    cancel,
	}, nil
}

func (c *Client) Close() error {
	err := c.mcpClient.Close()
	c.cancel()
	return err
}

func (c *Client) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	req := mcp.ListToolsRequest{}
	res, err := c.mcpClient.ListTools(ctx, req)
	if err != nil {
		return nil, err
	}
	return res.Tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	return c.mcpClient.CallTool(ctx, req)
}

func ToolToOpenAIFunction(tool mcp.Tool) openai.FunctionDefinition {
	var params any = map[string]interface{}{"type": "object"}
	if b, err := json.Marshal(tool.InputSchema); err == nil {
		var decoded any
		if err := json.Unmarshal(b, &decoded); err == nil && decoded != nil {
			params = decoded
		}
	}

	return openai.FunctionDefinition{
		Name:        tool.Name,
		Description: tool.Description,
		Parameters:  params,
	}
}

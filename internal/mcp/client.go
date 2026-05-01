package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sashabaranov/go-openai"
)

type Client struct {
	mcpClient client.MCPClient
	cancel    context.CancelFunc
}

func NewClient(ctx context.Context, command string, args []string, env []string) (*Client, error) {
	ctx, cancel := context.WithCancel(ctx)

	mcpClient, err := client.NewStdioMCPClient(command, env, args...)
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
		cancel()
		return nil, fmt.Errorf("failed to initialize MCP client: %w", err)
	}

	if res.ProtocolVersion == "" {
		return nil, fmt.Errorf("initialization failed, no protocol version returned")
	}

	return &Client{
		mcpClient: mcpClient,
		cancel:    cancel,
	}, nil
}

func (c *Client) Close() error {
	c.cancel()
	return c.mcpClient.Close()
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
	var params any
	// Convert InputSchema into any for OpenAI
	// mcp.Tool.InputSchema usually contains JSON schema as a struct or interface
	b, _ := json.Marshal(tool.InputSchema)
	_ = json.Unmarshal(b, &params)

	return openai.FunctionDefinition{
		Name:        tool.Name,
		Description: tool.Description,
		Parameters:  params,
	}
}

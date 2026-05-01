package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/korjavin/linklog/internal/mcp"
	"github.com/sashabaranov/go-openai"
)

type Service struct {
	client       *openai.Client
	mcpClient    *mcp.Client
	collectionID string
	model        string
}

func NewService(client *openai.Client, mcpClient *mcp.Client, collectionID, model string) *Service {
	if model == "" {
		model = openai.GPT4o
	}
	return &Service{
		client:       client,
		mcpClient:    mcpClient,
		collectionID: collectionID,
		model:        model,
	}
}

func (s *Service) ProcessInteraction(ctx context.Context, userInput string) (string, string, error) {
	tools, err := s.mcpClient.ListTools(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to list tools: %w", err)
	}

	var openaiTools []openai.Tool
	for _, t := range tools {
		fn := mcp.ToolToOpenAIFunction(t)
		openaiTools = append(openaiTools, openai.Tool{
			Type:     openai.ToolTypeFunction,
			Function: &fn,
		})
	}

	systemPrompt := fmt.Sprintf(`You are an assistant that manages documents in Outline.
You have access to a specific collection with ID: %s
Your goal is to organize links, notes, and contacts for the user.
Please use the provided tools to interact with Outline.
If creating or moving a document, default to placing it in the provided collection ID.`, s.collectionID)

	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: userInput,
		},
	}

	var finalReply string
	for {
		req := openai.ChatCompletionRequest{
			Model:    s.model,
			Messages: messages,
		}
		if len(openaiTools) > 0 {
			req.Tools = openaiTools
		}

		resp, err := s.client.CreateChatCompletion(ctx, req)
		if err != nil {
			return "", "", fmt.Errorf("chat completion error: %w", err)
		}

		msg := resp.Choices[0].Message
		messages = append(messages, msg)

		if len(msg.ToolCalls) == 0 {
			finalReply = msg.Content
			break
		}

		for _, toolCall := range msg.ToolCalls {
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				messages = append(messages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    fmt.Sprintf("Error parsing arguments: %v", err),
					Name:       toolCall.Function.Name,
					ToolCallID: toolCall.ID,
				})
				continue
			}

			result, err := s.mcpClient.CallTool(ctx, toolCall.Function.Name, args)
			var toolResultContent string
			if err != nil {
				toolResultContent = fmt.Sprintf("Tool call failed: %v", err)
			} else {
				if result.IsError {
					toolResultContent = "Tool returned error"
				} else {
					b, _ := json.Marshal(result.Content)
					toolResultContent = string(b)
				}
			}

			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    toolResultContent,
				Name:       toolCall.Function.Name,
				ToolCallID: toolCall.ID,
			})
		}
	}

	datePrompt := fmt.Sprintf(`Based on the conversation above, when should we follow up with this contact/topic?
Please respond with ONLY a date in YYYY-MM-DD format, or "none" if no follow-up is needed.
If it's a new contact or link, default to 1 week from today. Today is %s`, time.Now().Format("2006-01-02"))

	dateMessages := append([]openai.ChatCompletionMessage{}, messages...)
	dateMessages = append(dateMessages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: datePrompt,
	})

	dateReq := openai.ChatCompletionRequest{
		Model:    s.model,
		Messages: dateMessages,
	}

	dateResp, err := s.client.CreateChatCompletion(ctx, dateReq)
	if err != nil {
		return finalReply, "", fmt.Errorf("failed to get next contact date: %w", err)
	}

	suggestedDate := dateResp.Choices[0].Message.Content

	return finalReply, suggestedDate, nil
}

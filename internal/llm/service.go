package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/korjavin/linklog/internal/mcp"
	"github.com/sashabaranov/go-openai"
)

const maxToolIterations = 10

type Service struct {
	client       *openai.Client
	mcpClient    *mcp.Client
	collectionID string
	model        string
}

type FollowUp struct {
	Contact string
	Date    string
}

func NewService(client *openai.Client, mcpClient *mcp.Client, collectionID, model string) *Service {
	return &Service{
		client:       client,
		mcpClient:    mcpClient,
		collectionID: collectionID,
		model:        model,
	}
}

func (s *Service) ProcessInteraction(ctx context.Context, userInput string) (string, FollowUp, error) {
	tools, err := s.mcpClient.ListTools(ctx)
	if err != nil {
		return "", FollowUp{}, fmt.Errorf("failed to list tools: %w", err)
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
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: userInput},
	}

	var finalReply string
	completed := false
	for i := 0; i < maxToolIterations; i++ {
		req := openai.ChatCompletionRequest{
			Model:    s.model,
			Messages: messages,
		}
		if len(openaiTools) > 0 {
			req.Tools = openaiTools
		}

		resp, err := s.client.CreateChatCompletion(ctx, req)
		if err != nil {
			return "", FollowUp{}, fmt.Errorf("chat completion error: %w", err)
		}
		if len(resp.Choices) == 0 {
			return "", FollowUp{}, fmt.Errorf("LLM returned no choices")
		}

		msg := resp.Choices[0].Message
		messages = append(messages, msg)

		if len(msg.ToolCalls) == 0 {
			finalReply = msg.Content
			completed = true
			break
		}

		for _, toolCall := range msg.ToolCalls {
			toolResultContent := s.executeToolCall(ctx, toolCall)
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    toolResultContent,
				Name:       toolCall.Function.Name,
				ToolCallID: toolCall.ID,
			})
		}
	}

	if !completed {
		finalReply = "I ran out of steps before finishing — please try again or break the request into smaller parts."
		// Skip follow-up extraction: history is incomplete and may yield a misleading date.
		return finalReply, FollowUp{}, nil
	}

	if finalReply == "" {
		finalReply = "Done."
	}

	followUp := s.askFollowUp(ctx, messages)
	return finalReply, followUp, nil
}

func (s *Service) executeToolCall(ctx context.Context, toolCall openai.ToolCall) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return fmt.Sprintf("Error parsing arguments: %v", err)
	}

	result, err := s.mcpClient.CallTool(ctx, toolCall.Function.Name, args)
	if err != nil {
		return fmt.Sprintf("Tool call failed: %v", err)
	}
	b, err := json.Marshal(result.Content)
	if err != nil {
		return fmt.Sprintf("Failed to serialize tool result: %v", err)
	}
	if result.IsError {
		return fmt.Sprintf("Tool returned error: %s", string(b))
	}
	return string(b)
}

func (s *Service) askFollowUp(ctx context.Context, history []openai.ChatCompletionMessage) FollowUp {
	defaultDate := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	prompt := fmt.Sprintf(`Based on the conversation above, respond with a single JSON object (no prose, no code fences) of the form:
{"contact": "<short name of the contact or topic to follow up with>", "date": "YYYY-MM-DD"}
If no follow-up is needed, set date to "none". If a date is implied but unclear, default to %s. Today is %s.`,
		defaultDate, time.Now().Format("2006-01-02"))

	messages := append([]openai.ChatCompletionMessage{}, history...)
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: prompt,
	})

	resp, err := s.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:    s.model,
		Messages: messages,
	})
	if err != nil || len(resp.Choices) == 0 {
		return FollowUp{Date: defaultDate}
	}

	return parseFollowUp(resp.Choices[0].Message.Content, defaultDate)
}

var (
	dateRegex = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2})\b`)
	jsonRegex = regexp.MustCompile(`(?s)\{.*\}`)
	noneRegex = regexp.MustCompile(`(?i)\bnone\b`)
)

func parseFollowUp(raw, defaultDate string) FollowUp {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var parsed struct {
		Contact string `json:"contact"`
		Date    string `json:"date"`
	}
	fu := FollowUp{}
	candidate := raw
	if !strings.HasPrefix(candidate, "{") {
		if m := jsonRegex.FindString(raw); m != "" {
			candidate = m
		}
	}
	if err := json.Unmarshal([]byte(candidate), &parsed); err == nil {
		fu.Contact = strings.TrimSpace(parsed.Contact)
		fu.Date = strings.TrimSpace(parsed.Date)
	}

	if strings.EqualFold(fu.Date, "none") {
		fu.Date = ""
		return fu
	}

	// If JSON parsing failed entirely and the prose explicitly says "none", treat as no follow-up.
	if fu.Date == "" && fu.Contact == "" && noneRegex.MatchString(raw) {
		return fu
	}

	if _, err := time.Parse("2006-01-02", fu.Date); err != nil {
		// Only accept a date extracted from prose if it is today or in the future.
		// Past dates in prose are usually historical references ("last met 2025-01-15"),
		// not the next-contact date we're trying to capture.
		today := time.Now().Format("2006-01-02")
		if m := dateRegex.FindString(raw); m != "" && m >= today {
			if _, err := time.Parse("2006-01-02", m); err == nil {
				fu.Date = m
				return fu
			}
		}
		fu.Date = defaultDate
	}
	return fu
}

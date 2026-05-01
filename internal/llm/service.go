package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/korjavin/linklog/internal/mcp"
	"github.com/korjavin/linklog/internal/outline"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/sashabaranov/go-openai"
)

const maxToolIterations = 10

type Service struct {
	client       *openai.Client
	mcpClient    *mcp.Client
	outClient    *outline.Client
	collectionID string
	model        string
	// protectedDocID is the bot-managed schedule document. The bot owns its
	// content via the Outline REST API; the LLM agent must never touch it,
	// even when it happens to live inside the configured collection (in which
	// case the collection-membership check would otherwise let it through).
	protectedDocID string
}

type FollowUp struct {
	Contact string
	Date    string
}

func NewService(client *openai.Client, mcpClient *mcp.Client, outClient *outline.Client, collectionID, model, protectedDocID string) *Service {
	return &Service{
		client:         client,
		mcpClient:      mcpClient,
		outClient:      outClient,
		collectionID:   collectionID,
		model:          model,
		protectedDocID: protectedDocID,
	}
}

func (s *Service) ProcessInteraction(ctx context.Context, userInput string) (string, FollowUp, error) {
	tools, err := s.mcpClient.ListTools(ctx)
	if err != nil {
		return "", FollowUp{}, fmt.Errorf("failed to list tools: %w", err)
	}

	schemas := make(map[string]mcpgo.ToolInputSchema, len(tools))
	var openaiTools []openai.Tool
	for _, t := range tools {
		if !s.toolAllowed(t) {
			continue
		}
		schemas[t.Name] = t.InputSchema
		fn := mcp.ToolToOpenAIFunction(t)
		openaiTools = append(openaiTools, openai.Tool{
			Type:     openai.ToolTypeFunction,
			Function: &fn,
		})
	}

	systemPrompt := fmt.Sprintf(`You are an assistant that manages documents in Outline.
You have access to a single collection with ID: %s
ALL document reads, creates, updates, deletes, moves, and comments MUST stay inside this collection.
Never list, inspect, or modify other collections. Never operate on a documentId you have not first
located inside the configured collection (e.g., via search or list scoped to the collection).
When a tool accepts a collection identifier, always pass %[1]s explicitly.`, s.collectionID)

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
			toolResultContent := s.executeToolCall(ctx, toolCall, schemas)
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

func (s *Service) executeToolCall(ctx context.Context, toolCall openai.ToolCall, schemas map[string]mcpgo.ToolInputSchema) string {
	// Refuse any tool name the model invents or that we never exposed. Without
	// this, schemas[unknown] yields the zero schema and execution falls through
	// to mcpClient.CallTool — meaning a hallucinated `list_users` (or any tool
	// filtered out by toolAllowed) would still run if it doesn't happen to match
	// a denylist pattern.
	schema, allowed := schemas[toolCall.Function.Name]
	if !allowed {
		return fmt.Sprintf("Tool %q is not in the allowed set; refusing to execute.", toolCall.Function.Name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return fmt.Sprintf("Error parsing arguments: %v", err)
	}
	// json.Unmarshal of the literal "null" succeeds with args == nil. The scope
	// guards below short-circuit on nil args, so without this normalization a
	// model could bypass collection injection by sending `null` for arguments
	// to a tool whose collection_id is optional. Treat nil as an empty object
	// so enforceCollectionScope can still inject the configured ID.
	if args == nil {
		args = map[string]interface{}{}
	}

	if reason, ok := s.deniedTool(toolCall.Function.Name); ok {
		return fmt.Sprintf("Tool denied by collection-scope policy: %s", reason)
	}

	s.enforceCollectionScope(args, schema)

	if reason, ok := s.validateDocumentScope(ctx, toolCall.Function.Name, args); !ok {
		return fmt.Sprintf("Tool denied by collection-scope policy: %s", reason)
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

// crossWorkspaceToolPatterns lists tool name fragments that indicate a tool
// operates across collections or the entire workspace (collection ops,
// trash/recent listings, exports, RAG/ask). The bot is intentionally scoped to
// a single collection, so these are denied even if the schema happens to
// declare a documentId — the operation itself implicitly targets out-of-scope
// content (e.g., list_trash returns trashed docs from anywhere; export_all_*
// dumps everything; ask_* runs RAG over the whole workspace).
//
// Note: scoped read tools whose names contain "collection" but take a
// collection_id (e.g., get_collection, get_collection_structure,
// get_collection_documents) are NOT denied — enforceCollectionScope rewrites
// their collection_id to the configured one, so they can only inspect the
// scoped collection. Only collection ops that are inherently workspace-level
// (list/create/update/delete) are blocked here.
var crossWorkspaceToolPatterns = []string{
	"list_collections",
	"list-collections",
	"listcollections",
	"create_collection",
	"create-collection",
	"createcollection",
	"update_collection",
	"update-collection",
	"updatecollection",
	"delete_collection",
	"delete-collection",
	"deletecollection",
	"trash",
	"recent",
	"export",
	"_ask",
	"-ask",
	"ask_",
	"ask-",
}

func (s *Service) deniedTool(name string) (string, bool) {
	if s.collectionID == "" {
		return "", false
	}
	lower := strings.ToLower(name)
	for _, pat := range crossWorkspaceToolPatterns {
		if strings.Contains(lower, pat) {
			return fmt.Sprintf("tool %q targets out-of-scope content; bot is scoped to %s", name, s.collectionID), true
		}
	}
	return "", false
}

// toolAllowed decides whether a tool should be exposed to the LLM at all.
// A tool is only exposed when:
//   - its name is not on the cross-workspace denylist, AND
//   - its schema declares at least one scope anchor (collection_id or
//     document_id, possibly nested inside an array/object), OR a bare top-level
//     `id` when the tool name identifies a singular document operation
//     (so validateDocumentScope can still gate the call).
//
// This prevents the LLM from ever seeing tools that cannot be confined to the
// configured collection (e.g., list_recent_documents, list_users,
// export_all_collections), defending against prompt injection and model error.
// When the bot is unscoped (collectionID == ""), no filter is applied.
func (s *Service) toolAllowed(t mcpgo.Tool) bool {
	if s.collectionID == "" {
		return true
	}
	if _, denied := s.deniedTool(t.Name); denied {
		return false
	}
	if schemaHasScopeAnchor(t.InputSchema) {
		return true
	}
	// A bare top-level `id` is too ambiguous to anchor scope on its own at the
	// schema level, but when the tool name identifies a singular document
	// operation (get_document, update_document, ...) and the schema declares
	// `id`, validateDocumentScope verifies the document is in scope before the
	// call runs. Allow such tools through so scoped reads/updates still work.
	if _, hasID := t.InputSchema.Properties["id"]; hasID && looksLikeDocumentIDKey("id", t.Name) {
		return true
	}
	return false
}

// schemaHasScopeAnchor reports whether the tool's input schema contains a
// property anywhere in its tree (including inside `items` of arrays and nested
// `properties` of objects) whose name is a collection or document identifier.
func schemaHasScopeAnchor(schema mcpgo.ToolInputSchema) bool {
	return schemaPropsHaveScopeAnchor(schema.Properties)
}

func schemaPropsHaveScopeAnchor(props map[string]any) bool {
	for name, sub := range props {
		if isCollectionIDKey(name) || isDocumentIDPropName(name) {
			return true
		}
		if subMap, ok := sub.(map[string]any); ok {
			if schemaNodeHasScopeAnchor(subMap) {
				return true
			}
		}
	}
	return false
}

func schemaNodeHasScopeAnchor(node map[string]any) bool {
	if props, ok := node["properties"].(map[string]any); ok {
		if schemaPropsHaveScopeAnchor(props) {
			return true
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		if schemaNodeHasScopeAnchor(items) {
			return true
		}
	}
	return false
}

// isDocumentIDPropName matches schema property names that unambiguously hold
// one or more document identifiers — singular (documentId, parentDocumentId),
// plural arrays (documentIds), and snake_case variants. A bare "id" is excluded
// here: at top level it is disambiguated by the tool name (see
// looksLikeDocumentIDKey), but inside nested structures it is too ambiguous to
// anchor scope on. Plural and parent variants are included so batch tools
// (e.g., batch_move_documents with documentIds + parentDocumentId) get the
// same scope validation as single-document tools.
func isDocumentIDPropName(name string) bool {
	lk := strings.ToLower(name)
	switch lk {
	case "documentid", "document_id",
		"documentids", "document_ids",
		"parentdocumentid", "parent_document_id":
		return true
	}
	return false
}

// enforceCollectionScope forces every tool call into the configured collection.
// It (a) rewrites any collection-id-like field, anywhere in args, to the
// configured ID, and (b) injects the configured ID into args wherever the
// tool's input schema declares a collection-id property — including nested
// objects (e.g., `filter.collection_id`) — materializing missing intermediate
// objects as needed. (b) closes the gap where a tool accepts an OPTIONAL
// collection filter (top-level or nested) and the model omits it — without
// injection, the call would search or list across the workspace.
func (s *Service) enforceCollectionScope(args map[string]interface{}, schema mcpgo.ToolInputSchema) {
	if s.collectionID == "" || args == nil {
		return
	}

	injectCollectionFromSchema(args, schema.Properties, s.collectionID)
	rewriteCollectionFields(args, s.collectionID)
}

// injectCollectionFromSchema walks `props` recursively. For each declared
// collection-id key it ensures `args` carries the configured collection ID at
// that location, creating any missing intermediate objects. Array `items` are
// not traversed: there is no obvious slot to materialize for a missing array,
// and the rewriter still corrects any wrong values inside arrays the model
// does send.
func injectCollectionFromSchema(args map[string]interface{}, props map[string]any, collectionID string) {
	for propName, sub := range props {
		if isCollectionIDKey(propName) {
			if existing, ok := args[propName].(string); !ok || existing != collectionID {
				args[propName] = collectionID
			}
			continue
		}
		subMap, ok := sub.(map[string]any)
		if !ok {
			continue
		}
		nestedProps, hasProps := subMap["properties"].(map[string]any)
		if !hasProps || !propsHaveCollectionID(nestedProps) {
			continue
		}
		child, isMap := args[propName].(map[string]interface{})
		if !isMap {
			child = map[string]interface{}{}
			args[propName] = child
		}
		injectCollectionFromSchema(child, nestedProps, collectionID)
	}
}

// propsHaveCollectionID reports whether the schema sub-tree contains any
// collection-id key. Used to decide whether a missing nested object is worth
// materializing — we only create one when injection would actually happen.
func propsHaveCollectionID(props map[string]any) bool {
	for name, sub := range props {
		if isCollectionIDKey(name) {
			return true
		}
		subMap, ok := sub.(map[string]any)
		if !ok {
			continue
		}
		if nested, ok := subMap["properties"].(map[string]any); ok {
			if propsHaveCollectionID(nested) {
				return true
			}
		}
	}
	return false
}

func rewriteCollectionFields(v interface{}, collectionID string) {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, child := range t {
			if isCollectionIDKey(k) {
				// Rewrite any string-valued collection key (including the
				// empty string) to the configured ID. A non-string value is
				// likely a different shape (object/array) and is left alone.
				if str, ok := child.(string); ok && str != collectionID {
					t[k] = collectionID
				}
				continue
			}
			rewriteCollectionFields(child, collectionID)
		}
	case []interface{}:
		for _, child := range t {
			rewriteCollectionFields(child, collectionID)
		}
	}
}

func isCollectionIDKey(key string) bool {
	lk := strings.ToLower(key)
	return lk == "collection_id" || lk == "collectionid" || lk == "collection"
}

// validateDocumentScope rejects tool calls that target a documentId outside
// the configured collection. It walks the args recursively so batch tools
// (e.g., updates[].documentId) are validated, not just top-level fields. For
// each document identifier discovered, it looks up the document via the
// Outline REST API and compares its collectionId. This adds one HTTP call per
// document reference, which is acceptable in exchange for a real scope
// boundary that does not depend on the model staying in line.
//
// Property name handling:
//   - documentId / document_id are unambiguous and always validated, at any
//     depth.
//   - A bare "id" is only validated at top level, and only when the tool
//     name identifies a single-document operation (e.g., get_document). At
//     nested depths "id" is too ambiguous (could be a comment, attachment,
//     user, etc.) so it is skipped to avoid spurious lookups.
//
// Returns (reason, false) on denial.
func (s *Service) validateDocumentScope(ctx context.Context, toolName string, args map[string]interface{}) (string, bool) {
	if s.collectionID == "" || s.outClient == nil || args == nil {
		return "", true
	}

	if raw, ok := args["id"]; ok && looksLikeDocumentIDKey("id", toolName) {
		if docID, ok := raw.(string); ok && docID != "" {
			if reason, ok := s.checkDocumentInScope(ctx, docID); !ok {
				return reason, false
			}
		}
	}
	return s.walkArgsForDocumentIDs(ctx, args)
}

// isProtectedDocID reports whether the given document ID refers to a
// bot-managed document the LLM must not touch (currently the schedule doc).
func (s *Service) isProtectedDocID(docID string) bool {
	return s.protectedDocID != "" && docID == s.protectedDocID
}

func (s *Service) walkArgsForDocumentIDs(ctx context.Context, v interface{}) (string, bool) {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, child := range t {
			if isDocumentIDPropName(k) {
				if reason, ok := s.checkDocumentValue(ctx, child); !ok {
					return reason, false
				}
				continue
			}
			if reason, ok := s.walkArgsForDocumentIDs(ctx, child); !ok {
				return reason, false
			}
		}
	case []interface{}:
		for _, child := range t {
			if reason, ok := s.walkArgsForDocumentIDs(ctx, child); !ok {
				return reason, false
			}
		}
	}
	return "", true
}

// checkDocumentValue validates a value that came from a property whose name
// identifies a document reference. The value may be a single ID string
// (documentId, parentDocumentId) or an array of ID strings (documentIds).
func (s *Service) checkDocumentValue(ctx context.Context, v interface{}) (string, bool) {
	switch val := v.(type) {
	case string:
		if val == "" {
			return "", true
		}
		return s.checkDocumentInScope(ctx, val)
	case []interface{}:
		for _, item := range val {
			str, ok := item.(string)
			if !ok || str == "" {
				continue
			}
			if reason, ok := s.checkDocumentInScope(ctx, str); !ok {
				return reason, false
			}
		}
	}
	return "", true
}

func (s *Service) checkDocumentInScope(ctx context.Context, docID string) (string, bool) {
	if s.isProtectedDocID(docID) {
		return fmt.Sprintf("document %q is bot-managed schedule state and is off-limits to the agent", docID), false
	}
	actual, err := s.outClient.DocumentCollectionID(ctx, docID)
	if err != nil {
		// Fail closed: if we cannot prove the document is in scope, refuse.
		return fmt.Sprintf("could not verify document %q is in collection %s: %v", docID, s.collectionID, err), false
	}
	if actual != s.collectionID {
		return fmt.Sprintf("document %q lives in collection %s, but bot is scoped to %s", docID, actual, s.collectionID), false
	}
	return "", true
}

// looksLikeDocumentIDKey identifies argument keys that hold an Outline document
// ID. documentId/document_id are unambiguous. A bare "id" is only treated as a
// document key when the tool name contains "document" as a SINGULAR token
// (e.g., get_document, update-document, moveDocument). Plural "documents"
// (e.g., list_documents, search_documents) names a collection of docs, not a
// single one — those tools take filters, not a documentId. Tool names that
// operate on other entity types (users, comments, collections) keep their
// "id" untouched.
func looksLikeDocumentIDKey(propName, toolName string) bool {
	lk := strings.ToLower(propName)
	if lk == "documentid" || lk == "document_id" {
		return true
	}
	if lk != "id" {
		return false
	}
	tn := strings.ToLower(toolName)
	// Find any occurrence of "document" not immediately followed by 's' (which
	// would make it the plural resource name).
	for idx := 0; idx < len(tn); {
		i := strings.Index(tn[idx:], "document")
		if i < 0 {
			return false
		}
		end := idx + i + len("document")
		if end >= len(tn) || tn[end] != 's' {
			return true
		}
		idx = end
	}
	return false
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

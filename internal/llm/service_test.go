package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/korjavin/linklog/internal/mcp"
	"github.com/korjavin/linklog/internal/outline"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
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

func TestParseFollowUpEmptyDateInJSONIsNoFollowUp(t *testing.T) {
	// When the model returns valid JSON with an empty date and a contact name,
	// it almost certainly means "no follow-up" — not "use the default date for
	// this contact". Make sure we don't synthesize defaultDate in that case.
	fu := parseFollowUp(`{"contact":"Alice","date":""}`, "2026-12-31")
	assert.Equal(t, "", fu.Date)
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

func TestEnforceCollectionScopeRewritesAndInjects(t *testing.T) {
	s := &Service{collectionID: "scoped-collection"}
	schema := mcpgo.ToolInputSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"collectionId": map[string]interface{}{"type": "string"},
			"query":        map[string]interface{}{"type": "string"},
		},
	}

	// (a) Wrong collectionId is rewritten.
	args := map[string]interface{}{"collectionId": "other", "query": "foo"}
	s.enforceCollectionScope(args, schema)
	assert.Equal(t, "scoped-collection", args["collectionId"])

	// (b) Missing collectionId is injected when schema declares it.
	args = map[string]interface{}{"query": "foo"}
	s.enforceCollectionScope(args, schema)
	assert.Equal(t, "scoped-collection", args["collectionId"])

	// (c) Nested collection_id in arbitrary structure is rewritten.
	args = map[string]interface{}{
		"filter": map[string]interface{}{"collection_id": "other"},
	}
	s.enforceCollectionScope(args, mcpgo.ToolInputSchema{})
	nested := args["filter"].(map[string]interface{})
	assert.Equal(t, "scoped-collection", nested["collection_id"])
}

func TestDeniedToolBlocksCrossWorkspaceTools(t *testing.T) {
	s := &Service{collectionID: "scoped-collection"}
	for _, name := range []string{
		"list_collections", "list-collections", "ListCollections",
		"delete_collection", "outline_list_collections",
		// Workspace-wide listings, exports, and RAG.
		"list_recent_documents", "list_trash", "restore_from_trash",
		"export_all_collections", "export_collection",
		"ask_documents", "ask-outline",
	} {
		_, denied := s.deniedTool(name)
		assert.True(t, denied, "expected %s to be denied", name)
	}
	for _, name := range []string{
		"search_documents", "list_documents", "create_document", "update_document",
		// Scoped collection reads must NOT be denied — their collection_id is
		// rewritten by enforceCollectionScope, so they can only inspect the
		// configured collection.
		"get_collection", "get_collection_structure", "get_collection_documents",
	} {
		_, denied := s.deniedTool(name)
		assert.False(t, denied, "expected %s to NOT be denied", name)
	}
}

func TestSchemaHasScopeAnchor(t *testing.T) {
	cases := []struct {
		name   string
		schema mcpgo.ToolInputSchema
		want   bool
	}{
		{
			name: "top-level collectionId",
			schema: mcpgo.ToolInputSchema{Properties: map[string]any{
				"collectionId": map[string]any{"type": "string"},
				"query":        map[string]any{"type": "string"},
			}},
			want: true,
		},
		{
			name: "top-level documentId",
			schema: mcpgo.ToolInputSchema{Properties: map[string]any{
				"documentId": map[string]any{"type": "string"},
			}},
			want: true,
		},
		{
			name: "nested documentId in array items",
			schema: mcpgo.ToolInputSchema{Properties: map[string]any{
				"updates": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"documentId": map[string]any{"type": "string"},
							"text":       map[string]any{"type": "string"},
						},
					},
				},
			}},
			want: true,
		},
		{
			name: "nested collection_id under filter object",
			schema: mcpgo.ToolInputSchema{Properties: map[string]any{
				"filter": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"collection_id": map[string]any{"type": "string"},
					},
				},
			}},
			want: true,
		},
		{
			name: "no scope anchor (workspace-wide)",
			schema: mcpgo.ToolInputSchema{Properties: map[string]any{
				"limit":  map[string]any{"type": "integer"},
				"offset": map[string]any{"type": "integer"},
			}},
			want: false,
		},
		{
			name: "bare id is not enough — too ambiguous",
			schema: mcpgo.ToolInputSchema{Properties: map[string]any{
				"id": map[string]any{"type": "string"},
			}},
			want: false,
		},
		{
			name:   "empty schema",
			schema: mcpgo.ToolInputSchema{},
			want:   false,
		},
	}
	for _, tc := range cases {
		got := schemaHasScopeAnchor(tc.schema)
		assert.Equalf(t, tc.want, got, "schemaHasScopeAnchor(%s)", tc.name)
	}
}

func TestToolAllowedFiltersUnscopeable(t *testing.T) {
	s := &Service{collectionID: "scoped-collection"}

	// Allowed: declares scope anchor and not on denylist.
	allowed := mcpgo.Tool{
		Name: "search_documents",
		InputSchema: mcpgo.ToolInputSchema{Properties: map[string]any{
			"collectionId": map[string]any{"type": "string"},
			"query":        map[string]any{"type": "string"},
		}},
	}
	assert.True(t, s.toolAllowed(allowed))

	// Denied: workspace-wide listing without any scope anchor.
	listRecent := mcpgo.Tool{
		Name: "list_recent_documents",
		InputSchema: mcpgo.ToolInputSchema{Properties: map[string]any{
			"limit": map[string]any{"type": "integer"},
		}},
	}
	assert.False(t, s.toolAllowed(listRecent))

	// Denied: name on denylist even if a documentId field is present.
	exportTool := mcpgo.Tool{
		Name: "export_all_collections",
		InputSchema: mcpgo.ToolInputSchema{Properties: map[string]any{
			"documentId": map[string]any{"type": "string"},
		}},
	}
	assert.False(t, s.toolAllowed(exportTool))

	// Denied: ask/RAG tool that ranges over the whole workspace.
	askTool := mcpgo.Tool{
		Name: "ask_documents",
		InputSchema: mcpgo.ToolInputSchema{Properties: map[string]any{
			"question": map[string]any{"type": "string"},
		}},
	}
	assert.False(t, s.toolAllowed(askTool))

	// Denied: tool with no parameters at all.
	listUsers := mcpgo.Tool{
		Name: "list_users",
		InputSchema: mcpgo.ToolInputSchema{Properties: map[string]any{
			"offset": map[string]any{"type": "integer"},
		}},
	}
	assert.False(t, s.toolAllowed(listUsers))

	// Unscoped bot: filter is disabled — every tool is allowed.
	unscoped := &Service{collectionID: ""}
	assert.True(t, unscoped.toolAllowed(listRecent))
}

func TestToolAllowedExposesSingularDocumentToolWithBareID(t *testing.T) {
	// A tool like `get_document` whose schema only declares a bare top-level
	// `id` must still be exposed: validateDocumentScope checks the document is
	// in the configured collection before the call runs, so the lookup is
	// safe. Without this carve-out, scoped reads of single documents are
	// silently filtered out.
	s := &Service{collectionID: "scoped-collection"}
	getDoc := mcpgo.Tool{
		Name: "get_document",
		InputSchema: mcpgo.ToolInputSchema{Properties: map[string]any{
			"id": map[string]any{"type": "string"},
		}},
	}
	assert.True(t, s.toolAllowed(getDoc))

	// Plural tool name — `id` is too ambiguous (could be a comment, user, ...)
	// and there is no other anchor, so the tool stays filtered out.
	listDocs := mcpgo.Tool{
		Name: "list_documents",
		InputSchema: mcpgo.ToolInputSchema{Properties: map[string]any{
			"id": map[string]any{"type": "string"},
		}},
	}
	assert.False(t, s.toolAllowed(listDocs))
}

func TestIsDocumentIDPropNameCoversPluralAndParent(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"documentId", true},
		{"document_id", true},
		{"documentIds", true}, // batch tools (e.g., batch_move_documents)
		{"document_ids", true},
		{"parentDocumentId", true}, // nested document parent
		{"parent_document_id", true},
		{"id", false}, // ambiguous — handled separately at top level
		{"name", false},
	}
	for _, tc := range cases {
		got := isDocumentIDPropName(tc.name)
		assert.Equalf(t, tc.want, got, "isDocumentIDPropName(%q)", tc.name)
	}
}

func TestSchemaHasScopeAnchorRecognizesPluralDocIDs(t *testing.T) {
	// A batch tool that takes documentIds + collectionId must be recognized as
	// scoped (so it is exposed) AND its documentIds must later be validated.
	schema := mcpgo.ToolInputSchema{Properties: map[string]any{
		"documentIds":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"collectionId": map[string]any{"type": "string"},
	}}
	assert.True(t, schemaHasScopeAnchor(schema))

	// parentDocumentId alone should also count as a scope anchor.
	parentOnly := mcpgo.ToolInputSchema{Properties: map[string]any{
		"parentDocumentId": map[string]any{"type": "string"},
	}}
	assert.True(t, schemaHasScopeAnchor(parentOnly))
}

func TestEnforceCollectionScopeInjectsNestedCollectionID(t *testing.T) {
	// A tool whose schema declares `filter.collection_id` is exposed by
	// schemaHasScopeAnchor's recursive walk. If the model calls it with `{}`
	// or `{"filter":{}}`, the configured collection_id must still be injected
	// at the nested location — otherwise the call runs workspace-wide.
	s := &Service{collectionID: "scoped-collection"}
	schema := mcpgo.ToolInputSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"filter": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"collection_id": map[string]interface{}{"type": "string"},
				},
			},
		},
	}

	// (a) Missing filter object is materialized with the configured ID.
	args := map[string]interface{}{}
	s.enforceCollectionScope(args, schema)
	nested, ok := args["filter"].(map[string]interface{})
	require.True(t, ok, "filter should be created")
	assert.Equal(t, "scoped-collection", nested["collection_id"])

	// (b) Empty filter object gets collection_id injected.
	args = map[string]interface{}{"filter": map[string]interface{}{}}
	s.enforceCollectionScope(args, schema)
	nested = args["filter"].(map[string]interface{})
	assert.Equal(t, "scoped-collection", nested["collection_id"])

	// (c) Empty-string collection_id at nested location gets rewritten.
	args = map[string]interface{}{
		"filter": map[string]interface{}{"collection_id": ""},
	}
	s.enforceCollectionScope(args, schema)
	nested = args["filter"].(map[string]interface{})
	assert.Equal(t, "scoped-collection", nested["collection_id"])
}

func TestEnforceCollectionScopeNormalizesNilArgs(t *testing.T) {
	// json.Unmarshal of "null" produces args == nil. Without normalization the
	// scope guards short-circuit and a tool with an optional collection_id
	// would be called workspace-wide. Prove that nil is replaced with an empty
	// map and the configured collection_id is then injected by the schema walk.
	s := &Service{collectionID: "scoped-collection"}
	schema := mcpgo.ToolInputSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"collectionId": map[string]interface{}{"type": "string"},
		},
	}

	args := map[string]interface{}{}
	s.enforceCollectionScope(args, schema)
	assert.Equal(t, "scoped-collection", args["collectionId"])
}

func TestIsProtectedDocIDBlocksScheduleDoc(t *testing.T) {
	// The schedule doc is bot-managed state. Even when it lives inside the
	// configured collection (which would otherwise pass the membership check),
	// the LLM agent must not be able to read or modify it via MCP tools.
	s := &Service{collectionID: "scoped-collection", protectedDocID: "schedule-doc-id"}
	assert.True(t, s.isProtectedDocID("schedule-doc-id"))
	assert.False(t, s.isProtectedDocID("other-doc-id"))
	assert.False(t, s.isProtectedDocID(""))

	// When no protected doc is configured, no document is protected.
	unprotected := &Service{collectionID: "scoped-collection"}
	assert.False(t, unprotected.isProtectedDocID("schedule-doc-id"))
}

func TestExecuteToolCallNormalizesNullArgs(t *testing.T) {
	// A model that emits the literal `null` for arguments must not bypass
	// scope enforcement. executeToolCall should normalize nil to an empty map
	// before the scope guards run. We can observe the normalization indirectly
	// by ensuring deniedTool is still consulted (so a denied tool with `null`
	// args is still refused, not forwarded to MCP).
	s := &Service{collectionID: "scoped-collection"}
	schemas := map[string]mcpgo.ToolInputSchema{
		"list_collections": {Properties: map[string]any{}},
	}
	toolCall := openai.ToolCall{
		ID:   "call_null",
		Type: openai.ToolTypeFunction,
		Function: openai.FunctionCall{
			Name:      "list_collections",
			Arguments: `null`,
		},
	}
	out := s.executeToolCall(context.Background(), toolCall, schemas)
	assert.Contains(t, out, "collection-scope policy")
}

func TestExecuteToolCallRejectsUnexposedToolName(t *testing.T) {
	// If the model invents a tool name (or asks for one we filtered out),
	// executeToolCall must refuse rather than fall through to mcpClient.CallTool.
	s := &Service{collectionID: "scoped-collection"}
	schemas := map[string]mcpgo.ToolInputSchema{
		"search_documents": {Properties: map[string]any{"collectionId": map[string]any{"type": "string"}}},
	}
	toolCall := openai.ToolCall{
		ID:   "call_1",
		Type: openai.ToolTypeFunction,
		Function: openai.FunctionCall{
			Name:      "list_users", // not in schemas
			Arguments: `{}`,
		},
	}
	out := s.executeToolCall(context.Background(), toolCall, schemas)
	assert.Contains(t, out, "not in the allowed set")
	assert.Contains(t, out, `"list_users"`)
}

func TestLooksLikeDocumentIDKey(t *testing.T) {
	cases := []struct {
		prop, tool string
		want       bool
	}{
		{"documentId", "anything", true},
		{"document_id", "anything", true},
		{"id", "get_document", true},
		{"id", "update-document", true},
		{"id", "moveDocument", true},
		{"id", "list_documents", false}, // plural — collection of documents, not a single doc
		{"id", "search_documents", false},
		{"id", "list_users", false},
		{"id", "create_comment", false},
		{"name", "get_document", false},
	}
	for _, tc := range cases {
		got := looksLikeDocumentIDKey(tc.prop, tc.tool)
		assert.Equalf(t, tc.want, got, "looksLikeDocumentIDKey(%q, %q)", tc.prop, tc.tool)
	}
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

	// Minimal env for the MCP subprocess — do not pass os.Environ() because it
	// would leak unrelated secrets (TELEGRAM_BOT_TOKEN, LLM_API_KEY, ...) loaded
	// from .env into the third-party `npx` process.
	mcpEnv := []string{
		"OUTLINE_API_KEY=" + outlineKey,
		"OUTLINE_API_URL=" + strings.TrimSuffix(outlineURL, "/") + "/api",
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
	}
	mcpClient, err := mcp.NewClient(ctx, "npx", []string{"-y", "--package=outline-mcp-server", "outline-mcp-server-stdio"}, mcpEnv)
	require.NoError(t, err)
	defer func() { _ = mcpClient.Close() }()

	openaiConfig := openai.DefaultConfig(apiKey)
	openaiConfig.BaseURL = baseURL
	openaiClient := openai.NewClientWithConfig(openaiConfig)

	collectionID := os.Getenv("OUTLINE_COLLECTION_ID")
	outClient := outline.NewClient(outlineKey, outlineURL)

	svc := NewService(openaiClient, mcpClient, outClient, collectionID, model, os.Getenv("SCHEDULE_DOC_ID"))

	reply, followUp, err := svc.ProcessInteraction(ctx, "Hello! Please list the collections in Outline. Do not create anything.")
	require.NoError(t, err)

	assert.NotEmpty(t, reply)
	t.Logf("LLM Reply: %s", reply)
	t.Logf("Follow-up: %+v", followUp)
}

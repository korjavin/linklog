package outline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/documents.info" {
			t.Errorf("Expected path /api/documents.info, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Expected Authorization header Bearer test-key, got %s", r.Header.Get("Authorization"))
		}

		resp := DocumentResponse{
			Data: Document{
				ID:    "test-id",
				Title: "Test Doc",
				Text:  "Test Text",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	doc, err := client.GetDocument(context.Background(), "test-id")
	if err != nil {
		t.Fatalf("Failed to get document: %v", err)
	}

	if doc.ID != "test-id" {
		t.Errorf("Expected id test-id, got %s", doc.ID)
	}
}

func TestUpdateDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/documents.update" {
			t.Errorf("Expected path /api/documents.update, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	err := client.UpdateDocument(context.Background(), "test-id", "new text")
	if err != nil {
		t.Fatalf("Failed to update document: %v", err)
	}
}

func TestScheduleTable(t *testing.T) {
	text := `| Contact | Next Contact Date |
| --- | --- |
| John Doe | 2026-05-10 |
| Jane Smith | 2026-05-15 |
`
	entries := ParseScheduleTable(text)
	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}
	if entries[0].Contact != "John Doe" || entries[0].Date != "2026-05-10" {
		t.Errorf("Unexpected entry 0: %+v", entries[0])
	}

	serialized := SerializeScheduleTable(entries)
	if !testing.Short() {
		t.Logf("Serialized table:\n%s", serialized)
	}

	// Re-parse and verify
	reParsed := ParseScheduleTable(serialized)
	if len(reParsed) != 2 {
		t.Fatalf("Expected 2 entries after re-parse, got %d", len(reParsed))
	}
}

func TestParseScheduleTableWithoutSeparatorTreatsAllRowsAsData(t *testing.T) {
	text := `| Alice | 2026-05-10 |
| Bob | 2026-06-01 |
`
	entries := ParseScheduleTable(text)
	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries when no separator row, got %d", len(entries))
	}
	if entries[0].Contact != "Alice" || entries[1].Contact != "Bob" {
		t.Errorf("Unexpected entries: %+v", entries)
	}
}

func TestParseScheduleTableKeepsRowsContainingContactWord(t *testing.T) {
	text := `| Contact | Next Contact Date |
| --- | --- |
| Acme Contact Sales | 2026-05-10 |
`
	entries := ParseScheduleTable(text)
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
	if entries[0].Contact != "Acme Contact Sales" {
		t.Errorf("Unexpected entry: %+v", entries[0])
	}
}

func TestReplaceScheduleTablePreservesSurroundingContent(t *testing.T) {
	original := `# Follow-up Schedule

This document tracks contacts to follow up with.

| Contact | Next Contact Date |
| --- | --- |
| Alice | 2026-05-10 |

Notes: keep this list trimmed.
`
	newTable := SerializeScheduleTable([]ScheduleEntry{
		{Contact: "Alice", Date: "2026-05-10"},
		{Contact: "Bob", Date: "2026-06-01"},
	})

	result := ReplaceScheduleTable(original, newTable)

	for _, expected := range []string{
		"# Follow-up Schedule",
		"This document tracks contacts to follow up with.",
		"Notes: keep this list trimmed.",
		"| Bob | 2026-06-01 |",
	} {
		if !strings.Contains(result, expected) {
			t.Errorf("expected result to contain %q, got:\n%s", expected, result)
		}
	}

	// Re-parsing the result should yield the new entries.
	entries := ParseScheduleTable(result)
	if len(entries) != 2 || entries[1].Contact != "Bob" {
		t.Errorf("re-parse mismatch: %+v", entries)
	}
}

func TestReplaceScheduleTableAppendsWhenNoTable(t *testing.T) {
	original := "# Schedule\n\nNo table yet.\n"
	newTable := SerializeScheduleTable([]ScheduleEntry{{Contact: "Alice", Date: "2026-05-10"}})

	result := ReplaceScheduleTable(original, newTable)

	if !strings.Contains(result, "# Schedule") || !strings.Contains(result, "| Alice | 2026-05-10 |") {
		t.Errorf("expected appended table, got:\n%s", result)
	}
}

func TestReplaceScheduleTableEmptyDoc(t *testing.T) {
	newTable := SerializeScheduleTable([]ScheduleEntry{{Contact: "Alice", Date: "2026-05-10"}})
	result := ReplaceScheduleTable("", newTable)
	if result != newTable {
		t.Errorf("expected newTable for empty doc, got: %q", result)
	}
}

func TestScheduleTableEscapesPipesAndNewlines(t *testing.T) {
	entries := []ScheduleEntry{
		{Contact: "Alice | Bob", Date: "2026-05-10"},
		{Contact: "Carol\nDavid", Date: "2026-06-01"},
	}
	serialized := SerializeScheduleTable(entries)
	if strings.Count(serialized, "\n") != 4 {
		t.Errorf("expected exactly 4 newlines (header, sep, 2 rows), got serialization:\n%s", serialized)
	}

	parsed := ParseScheduleTable(serialized)
	if len(parsed) != 2 {
		t.Fatalf("expected 2 entries after round-trip, got %d:\n%s", len(parsed), serialized)
	}
	if parsed[0].Contact != "Alice | Bob" || parsed[0].Date != "2026-05-10" {
		t.Errorf("entry 0 round-trip mismatch: %+v", parsed[0])
	}
	if parsed[1].Date != "2026-06-01" {
		t.Errorf("entry 1 date corrupted by newline: %+v", parsed[1])
	}
}

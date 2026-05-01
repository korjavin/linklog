package outline

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	doc, err := client.GetDocument("test-id")
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
	err := client.UpdateDocument("test-id", "new text")
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

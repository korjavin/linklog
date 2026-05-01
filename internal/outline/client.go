package outline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const httpTimeout = 30 * time.Second

type Client struct {
	apiKey  string
	baseURL string
	hc      *http.Client
}

func NewClient(apiKey, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		hc:      &http.Client{Timeout: httpTimeout},
	}
}

type DocumentResponse struct {
	Data Document `json:"data"`
}

type Document struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

func (c *Client) GetDocument(id string) (*Document, error) {
	var res DocumentResponse
	if err := c.post("/api/documents.info", map[string]string{"id": id}, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

func (c *Client) UpdateDocument(id, text string) error {
	return c.post("/api/documents.update", map[string]string{"id": id, "text": text}, nil)
}

func (c *Client) post(path string, body, out interface{}) error {
	reqBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("outline api error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type ScheduleEntry struct {
	Contact string
	Date    string
}

// isSeparatorRow reports whether the row is a GFM table separator (cells of dashes/colons).
func isSeparatorRow(line string) bool {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		for _, r := range t {
			if r != '-' && r != ':' {
				return false
			}
		}
	}
	return true
}

func ParseScheduleTable(text string) []ScheduleEntry {
	var entries []ScheduleEntry
	headerSeen := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		if isSeparatorRow(line) {
			headerSeen = true
			continue
		}
		if !headerSeen {
			// First pipe-row before the separator is treated as the header.
			headerSeen = true
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) >= 3 {
			contact := strings.TrimSpace(parts[1])
			date := strings.TrimSpace(parts[2])
			if contact != "" && date != "" {
				entries = append(entries, ScheduleEntry{Contact: contact, Date: date})
			}
		}
	}
	return entries
}

func SerializeScheduleTable(entries []ScheduleEntry) string {
	var sb strings.Builder
	sb.WriteString("| Contact | Next Contact Date |\n")
	sb.WriteString("| --- | --- |\n")
	for _, entry := range entries {
		fmt.Fprintf(&sb, "| %s | %s |\n", entry.Contact, entry.Date)
	}
	return sb.String()
}

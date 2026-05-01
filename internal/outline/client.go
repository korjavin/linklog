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

func (c *Client) GetDocument(ctx context.Context, id string) (*Document, error) {
	var res DocumentResponse
	if err := c.post(ctx, "/api/documents.info", map[string]string{"id": id}, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

func (c *Client) UpdateDocument(ctx context.Context, id, text string) error {
	return c.post(ctx, "/api/documents.update", map[string]string{"id": id, "text": text}, nil)
}

func (c *Client) post(ctx context.Context, path string, body, out interface{}) error {
	reqBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
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
	var pipeLines []string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "|") {
			pipeLines = append(pipeLines, line)
		}
	}

	// If a GFM separator row exists, the row immediately before it is the header
	// and must be skipped. If no separator exists, treat every pipe row as data
	// (so the first contact row isn't silently dropped).
	sepIdx := -1
	for i, line := range pipeLines {
		if isSeparatorRow(line) {
			sepIdx = i
			break
		}
	}

	var entries []ScheduleEntry
	for i, line := range pipeLines {
		if i == sepIdx {
			continue
		}
		if sepIdx > 0 && i == sepIdx-1 {
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

// ReplaceScheduleTable replaces the first markdown table region in text with newTable,
// preserving any content above or below it. If text contains no table, newTable is appended.
func ReplaceScheduleTable(text, newTable string) string {
	if text == "" {
		return newTable
	}
	lines := strings.Split(text, "\n")
	tableStart := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			tableStart = i
			break
		}
	}
	if tableStart == -1 {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		return text + newTable
	}
	tableEnd := tableStart
	for i := tableStart; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
			tableEnd = i
		} else {
			break
		}
	}

	var sb strings.Builder
	if tableStart > 0 {
		sb.WriteString(strings.Join(lines[:tableStart], "\n"))
		sb.WriteString("\n")
	}
	sb.WriteString(newTable)
	if tableEnd+1 < len(lines) {
		sb.WriteString(strings.Join(lines[tableEnd+1:], "\n"))
	}
	return sb.String()
}

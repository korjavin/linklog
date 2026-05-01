package outline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	apiKey  string
	baseURL string
	hc      *http.Client
}

func NewClient(apiKey, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		hc:      &http.Client{},
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
	url := fmt.Sprintf("%s/api/documents.info", c.baseURL)
	reqBody, _ := json.Marshal(map[string]string{"id": id})
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("outline api error (status %d): %s", resp.StatusCode, string(body))
	}

	var res DocumentResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	return &res.Data, nil
}

func (c *Client) UpdateDocument(id, text string) error {
	url := fmt.Sprintf("%s/api/documents.update", c.baseURL)
	reqBody, _ := json.Marshal(map[string]string{
		"id":   id,
		"text": text,
	})
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("outline api error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

type ScheduleEntry struct {
	Contact string
	Date    string
}

func ParseScheduleTable(text string) []ScheduleEntry {
	var entries []ScheduleEntry
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.Contains(line, "---") || strings.Contains(strings.ToLower(line), "contact") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) >= 3 {
			contact := strings.TrimSpace(parts[1])
			date := strings.TrimSpace(parts[2])
			if contact != "" && date != "" {
				entries = append(entries, ScheduleEntry{
					Contact: contact,
					Date:    date,
				})
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
		sb.WriteString(fmt.Sprintf("| %s | %s |\n", entry.Contact, entry.Date))
	}
	return sb.String()
}

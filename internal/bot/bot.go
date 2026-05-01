package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/korjavin/linklog/internal/llm"
	"github.com/korjavin/linklog/internal/outline"
	"gopkg.in/telebot.v3"
)

type Bot struct {
	tb          *telebot.Bot
	llmService  *llm.Service
	outClient   *outline.Client
	scheduleDoc string
	knownChats  map[string]int64
}

func NewBot(token string, llmService *llm.Service, outClient *outline.Client, scheduleDoc string) (*Bot, error) {
	pref := telebot.Settings{
		Token:  token,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}

	tb, err := telebot.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	b := &Bot{
		tb:          tb,
		llmService:  llmService,
		outClient:   outClient,
		scheduleDoc: scheduleDoc,
		knownChats:  make(map[string]int64),
	}

	b.setupHandlers()

	return b, nil
}

func (b *Bot) setupHandlers() {
	b.tb.Handle(telebot.OnText, b.handleText)
}

func (b *Bot) handleText(c telebot.Context) error {
	ctx := context.Background()
	userInput := c.Text()
	
	senderName := c.Sender().Username
	if senderName == "" {
		senderName = c.Sender().FirstName
	}
	
	b.knownChats[senderName] = c.Sender().ID

	finalReply, suggestedDate, err := b.llmService.ProcessInteraction(ctx, userInput)
	if err != nil {
		log.Printf("Error processing interaction: %v", err)
		return c.Send(fmt.Sprintf("Sorry, I encountered an error: %v", err))
	}

	if suggestedDate != "" && strings.ToLower(suggestedDate) != "none" {
		err := b.updateSchedule(senderName, suggestedDate)
		if err != nil {
			log.Printf("Error updating schedule: %v", err)
		}
	}

	if finalReply == "" {
		finalReply = "Done."
	}
	return c.Send(finalReply)
}

func (b *Bot) updateSchedule(contact, date string) error {
	doc, err := b.outClient.GetDocument(b.scheduleDoc)
	if err != nil {
		return fmt.Errorf("failed to get schedule doc: %w", err)
	}

	entries := outline.ParseScheduleTable(doc.Text)
	
	// Update existing entry or add new
	found := false
	for i, entry := range entries {
		if strings.EqualFold(entry.Contact, contact) {
			entries[i].Date = date
			found = true
			break
		}
	}
	
	if !found {
		entries = append(entries, outline.ScheduleEntry{
			Contact: contact,
			Date:    date,
		})
	}
	
	newTableText := outline.SerializeScheduleTable(entries)
	
	// Replace old table with new table in the document text
	// For simplicity, if the doc only contains the table, we just update the text.
	// In a real scenario we'd need to replace just the table part. 
	// The instructions imply a specific "schedule table" document, so we can assume it's mostly the table.
	newText := doc.Text
	if strings.Contains(newText, "| Contact |") {
		// find start of table and end of table and replace
		lines := strings.Split(newText, "\n")
		var newLines []string
		inTable := false
		replaced := false
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "|") {
				if !inTable {
					inTable = true
					if !replaced {
						newLines = append(newLines, newTableText)
						replaced = true
					}
				}
			} else {
				inTable = false
				newLines = append(newLines, line)
			}
		}
		newText = strings.Join(newLines, "\n")
	} else {
		newText = newText + "\n\n" + newTableText
	}
	
	return b.outClient.UpdateDocument(b.scheduleDoc, newText)
}

func (b *Bot) Start() {
	log.Println("Bot started listening...")
	b.tb.Start()
}

func (b *Bot) Stop() {
	log.Println("Bot stopping...")
	b.tb.Stop()
}

func (b *Bot) Notify(contact, message string) {
	if b.knownChats == nil {
		return
	}
	id, ok := b.knownChats[contact]
	if !ok {
		log.Printf("Cannot notify %s, chat ID unknown", contact)
		return
	}
	_, err := b.tb.Send(&telebot.User{ID: id}, message)
	if err != nil {
		log.Printf("Failed to notify %s: %v", contact, err)
	}
}

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

const (
	interactionTimeout = 2 * time.Minute
	telegramMaxLen     = 4000
)

type Bot struct {
	tb           *telebot.Bot
	llmService   *llm.Service
	outClient    *outline.Client
	scheduleDoc  string
	adminChatID  int64
}

func NewBot(token string, adminChatID int64, llmService *llm.Service, outClient *outline.Client, scheduleDoc string) (*Bot, error) {
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
		adminChatID: adminChatID,
	}

	tb.Handle(telebot.OnText, b.handleText)
	return b, nil
}

func (b *Bot) handleText(c telebot.Context) error {
	if c.Sender() == nil {
		return nil
	}
	if c.Sender().ID != b.adminChatID {
		log.Printf("Rejected message from unauthorized sender %d", c.Sender().ID)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), interactionTimeout)
	defer cancel()

	finalReply, followUp, err := b.llmService.ProcessInteraction(ctx, c.Text())
	if err != nil {
		log.Printf("Error processing interaction: %v", err)
		return c.Send("Sorry, I encountered an error processing your message. Check the bot logs for details.")
	}

	if followUp.Date != "" && followUp.Contact != "" {
		if err := b.upsertSchedule(ctx, followUp.Contact, followUp.Date); err != nil {
			log.Printf("Error updating schedule: %v", err)
			finalReply += "\n\n(note: failed to record the follow-up reminder)"
		}
	}

	if finalReply == "" {
		finalReply = "Done."
	}
	return c.Send(truncateForTelegram(finalReply))
}

func truncateForTelegram(s string) string {
	if len(s) <= telegramMaxLen {
		return s
	}
	const suffix = "\n\n[... truncated]"
	cutoff := telegramMaxLen - len(suffix)
	if cutoff < 0 {
		cutoff = 0
	}
	return s[:cutoff] + suffix
}

func (b *Bot) upsertSchedule(ctx context.Context, contact, date string) error {
	doc, err := b.outClient.GetDocument(ctx, b.scheduleDoc)
	if err != nil {
		return fmt.Errorf("failed to get schedule doc: %w", err)
	}

	entries := outline.ParseScheduleTable(doc.Text)

	found := false
	for i, entry := range entries {
		if strings.EqualFold(entry.Contact, contact) {
			entries[i].Date = date
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, outline.ScheduleEntry{Contact: contact, Date: date})
	}

	return b.outClient.UpdateDocument(ctx, b.scheduleDoc, outline.SerializeScheduleTable(entries))
}

func (b *Bot) Start() {
	b.tb.Start()
}

func (b *Bot) Stop() {
	b.tb.Stop()
}

// Notify sends a message to the configured admin chat.
func (b *Bot) Notify(message string) {
	if _, err := b.tb.Send(&telebot.Chat{ID: b.adminChatID}, message); err != nil {
		log.Printf("Failed to notify admin: %v", err)
	}
}

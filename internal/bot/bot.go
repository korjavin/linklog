package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/korjavin/linklog/internal/llm"
	"github.com/korjavin/linklog/internal/outline"
	"gopkg.in/telebot.v3"
)

const (
	interactionTimeout = 2 * time.Minute
	// telegramMaxUTF16 is Telegram's per-message cap, counted in UTF-16 code
	// units (not bytes or runes). Non-BMP runes — most modern emoji, math
	// symbols, some CJK extension blocks — take 2 code units each.
	telegramMaxUTF16 = 4096
)

type Bot struct {
	tb          *telebot.Bot
	llmService  *llm.Service
	outClient   *outline.Client
	scheduleDoc string
	adminChatID int64
	// scheduleMu serializes read-modify-write cycles on the schedule document.
	// Telebot dispatches handlers in their own goroutines by default, so two
	// nearly-simultaneous messages could otherwise both fetch the doc, each
	// apply only their own change, and race on the final UpdateDocument call —
	// dropping one of the reminders.
	scheduleMu sync.Mutex
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
	// Reject if not a private chat with the admin: prevents leaking responses
	// to a group/channel that the admin happens to be a member of.
	chat := c.Chat()
	if chat == nil || chat.Type != telebot.ChatPrivate || chat.ID != b.adminChatID {
		log.Print("Rejected message from non-private chat or wrong chat ID")
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
	const suffix = "\n\n[... truncated]"

	// Walk once, counting UTF-16 code units. A rune-based count under-counts
	// non-BMP characters (each takes 2 UTF-16 units), so a 4000-rune string of
	// emoji is 8000 units — well over Telegram's 4096 limit, which makes the
	// API reject the message and the user sees nothing.
	totalUnits := 0
	for _, r := range s {
		totalUnits += utf16.RuneLen(r)
	}
	if totalUnits <= telegramMaxUTF16 {
		return s
	}

	suffixUnits := 0
	for _, r := range suffix {
		suffixUnits += utf16.RuneLen(r)
	}
	budget := telegramMaxUTF16 - suffixUnits
	if budget < 0 {
		budget = 0
	}

	var b strings.Builder
	used := 0
	for _, r := range s {
		c := utf16.RuneLen(r)
		if used+c > budget {
			break
		}
		b.WriteRune(r)
		used += c
	}
	b.WriteString(suffix)
	return b.String()
}

func (b *Bot) upsertSchedule(ctx context.Context, contact, date string) error {
	b.scheduleMu.Lock()
	defer b.scheduleMu.Unlock()

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

	newTable := outline.SerializeScheduleTable(entries)
	return b.outClient.UpdateDocument(ctx, b.scheduleDoc, outline.ReplaceScheduleTable(doc.Text, newTable))
}

func (b *Bot) Start() {
	b.tb.Start()
}

func (b *Bot) Stop() {
	b.tb.Stop()
}

// Notify sends a message to the configured admin chat. Returns an error if
// the send fails so callers can decide whether to retry.
func (b *Bot) Notify(message string) error {
	_, err := b.tb.Send(&telebot.Chat{ID: b.adminChatID}, truncateForTelegram(message))
	if err != nil {
		log.Printf("Failed to notify admin: %v", err)
	}
	return err
}

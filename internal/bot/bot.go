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
	// maxContactBytes caps the UTF-8 byte length of a contact name embedded in
	// inline-button callback data. Telegram limits callback_data to 64 bytes;
	// the longest unique prefix is "snooze" (6 bytes) plus the "\a" separator
	// (1 byte), leaving 57 bytes for the payload.
	maxContactBytes = 57
)

type Bot struct {
	tb          *telebot.Bot
	llmService  *llm.Service
	outClient   *outline.Client
	scheduleDoc string
	adminChatID int64
	// scheduleMu serializes read-modify-write cycles on the schedule document.
	scheduleMu sync.Mutex
	// pendingContact holds the contact name the admin has committed to
	// summarizing after clicking "Done ✓" on a reminder. Empty = no pending.
	pendingContact string
	pendingMu      sync.Mutex
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
	tb.Handle("\fdone", b.handleDone)
	tb.Handle("\fsnooze", b.handleSnooze)
	return b, nil
}

func (b *Bot) isAdmin(c telebot.Context) bool {
	return c.Sender() != nil && c.Sender().ID == b.adminChatID
}

func (b *Bot) handleText(c telebot.Context) error {
	if !b.isAdmin(c) {
		log.Printf("Rejected message from unauthorized sender %d", c.Sender().ID)
		return nil
	}
	chat := c.Chat()
	if chat == nil || chat.Type != telebot.ChatPrivate || chat.ID != b.adminChatID {
		log.Print("Rejected message from non-private chat or wrong chat ID")
		return nil
	}

	b.pendingMu.Lock()
	pending := b.pendingContact
	b.pendingContact = ""
	b.pendingMu.Unlock()

	input := c.Text()
	if pending != "" {
		input = fmt.Sprintf("Update interaction with %s: %s", pending, input)
	}

	ctx, cancel := context.WithTimeout(context.Background(), interactionTimeout)
	defer cancel()

	finalReply, followUp, err := b.llmService.ProcessInteraction(ctx, input)
	if err != nil {
		log.Printf("Error processing interaction: %v", err)
		return b.sendChunks(c.Chat(), splitForTelegram("Sorry, I encountered an error processing your message. Check the bot logs for details."), nil)
	}

	if followUp.Date != "" && followUp.Contact != "" {
		if err := b.upsertSchedule(ctx, followUp.Contact, followUp.Date, followUp.Topic); err != nil {
			log.Printf("Error updating schedule: %v", err)
			finalReply += "\n\n(note: failed to record the follow-up reminder)"
		}
	}

	if finalReply == "" {
		finalReply = "Done."
	}
	return b.sendChunks(c.Chat(), splitForTelegram(finalReply), nil)
}

func (b *Bot) handleDone(c telebot.Context) error {
	if !b.isAdmin(c) {
		return c.Respond()
	}
	contact := c.Callback().Data

	b.pendingMu.Lock()
	b.pendingContact = contact
	b.pendingMu.Unlock()

	_ = c.Respond(&telebot.CallbackResponse{Text: "Send a quick summary of your interaction."})
	_, err := b.tb.Send(&telebot.Chat{ID: b.adminChatID},
		fmt.Sprintf("What happened with %s? Send a quick summary and I'll update the record.", contact))
	return err
}

func (b *Bot) handleSnooze(c telebot.Context) error {
	if !b.isAdmin(c) {
		return c.Respond()
	}
	contact := c.Callback().Data
	newDate := time.Now().AddDate(0, 0, 7).Format("2006-01-02")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := b.upsertSchedule(ctx, contact, newDate, ""); err != nil {
		log.Printf("Snooze: failed to update schedule for %q: %v", contact, err)
		_ = c.Respond(&telebot.CallbackResponse{Text: "Failed to snooze, check logs."})
		return err
	}

	_ = c.Respond(&telebot.CallbackResponse{Text: fmt.Sprintf("Snoozed until %s", newDate)})
	_, err := b.tb.Send(&telebot.Chat{ID: b.adminChatID},
		fmt.Sprintf("Snoozed %s — next reminder on %s.", contact, newDate))
	return err
}

func (b *Bot) upsertSchedule(ctx context.Context, contact, date, topic string) error {
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
			if topic != "" {
				entries[i].Topic = topic
			}
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, outline.ScheduleEntry{Contact: contact, Date: date, Topic: topic})
	}

	newTable := outline.SerializeScheduleTable(entries)
	return b.outClient.UpdateDocument(ctx, b.scheduleDoc, outline.ReplaceScheduleTable(doc.Text, newTable))
}

// SetNotifiedAt updates the NotifiedAt field for the given contact in the
// schedule doc. Called by the scheduler after a notification is sent so the
// state survives bot restarts.
func (b *Bot) SetNotifiedAt(ctx context.Context, contact, notifiedAt string) error {
	b.scheduleMu.Lock()
	defer b.scheduleMu.Unlock()

	doc, err := b.outClient.GetDocument(ctx, b.scheduleDoc)
	if err != nil {
		return fmt.Errorf("failed to get schedule doc: %w", err)
	}

	entries := outline.ParseScheduleTable(doc.Text)
	for i, entry := range entries {
		if strings.EqualFold(entry.Contact, contact) {
			entries[i].NotifiedAt = notifiedAt
			break
		}
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

// Notify sends a plain-text message to the admin, splitting across multiple
// messages if the content exceeds Telegram's 4096 UTF-16 unit limit.
func (b *Bot) Notify(message string) error {
	return b.sendChunks(&telebot.Chat{ID: b.adminChatID}, splitForTelegram(message), nil)
}

// NotifyWithButtons sends a reminder message with "Done ✓" and "Snooze 1w"
// inline buttons attached to the last chunk.
func (b *Bot) NotifyWithButtons(contact, message string) error {
	markup := &telebot.ReplyMarkup{}
	safeContact := truncateToUTF8Bytes(contact, maxContactBytes)
	doneBtn := markup.Data("Done ✓", "done", safeContact)
	snoozeBtn := markup.Data("Snooze 1w", "snooze", safeContact)
	markup.Inline(markup.Row(doneBtn, snoozeBtn))
	return b.sendChunks(&telebot.Chat{ID: b.adminChatID}, splitForTelegram(message), markup)
}

// sendChunks sends each chunk as a separate message. The optional markup is
// attached only to the last chunk so buttons appear at the end of the content.
func (b *Bot) sendChunks(to telebot.Recipient, chunks []string, markup *telebot.ReplyMarkup) error {
	for i, chunk := range chunks {
		var opts []interface{}
		if markup != nil && i == len(chunks)-1 {
			opts = append(opts, markup)
		}
		if _, err := b.tb.Send(to, chunk, opts...); err != nil {
			log.Printf("Failed to send message chunk %d/%d: %v", i+1, len(chunks), err)
			return err
		}
	}
	return nil
}

// splitForTelegram splits s into chunks that each fit within Telegram's 4096
// UTF-16 code unit limit. It prefers to cut at paragraph boundaries (\n\n),
// then at line boundaries (\n), and only hard-cuts when no boundary is found.
func splitForTelegram(s string) []string {
	if countUTF16(s) <= telegramMaxUTF16 {
		return []string{s}
	}
	var chunks []string
	for s != "" {
		if countUTF16(s) <= telegramMaxUTF16 {
			chunks = append(chunks, s)
			return chunks
		}
		end := byteIndexAtUTF16(s, telegramMaxUTF16)
		candidate := s[:end]

		if idx := strings.LastIndex(candidate, "\n\n"); idx > 0 {
			chunks = append(chunks, strings.TrimRight(s[:idx], " \t"))
			s = strings.TrimLeft(s[idx:], "\n")
			continue
		}
		if idx := strings.LastIndex(candidate, "\n"); idx > 0 {
			chunks = append(chunks, strings.TrimRight(s[:idx], " \t"))
			s = s[idx+1:]
			continue
		}
		chunks = append(chunks, candidate)
		s = s[end:]
	}
	return chunks
}

// countUTF16 returns the number of UTF-16 code units in s.
func countUTF16(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// byteIndexAtUTF16 returns the byte offset in s where the UTF-16 unit count
// first reaches limit, or len(s) if the whole string fits.
func byteIndexAtUTF16(s string, limit int) int {
	units := 0
	for i, r := range s {
		u := utf16.RuneLen(r)
		if units+u > limit {
			return i
		}
		units += u
	}
	return len(s)
}

// truncateToUTF8Bytes truncates s to at most max bytes, backing up to a valid
// UTF-8 rune boundary so the result is always valid UTF-8.
func truncateToUTF8Bytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	b := []byte(s)[:max]
	// Walk back past any UTF-8 continuation bytes (10xxxxxx).
	for len(b) > 0 && b[len(b)-1]&0xC0 == 0x80 {
		b = b[:len(b)-1]
	}
	// If the last byte is now a leading byte of a multi-byte sequence, drop it too.
	if len(b) > 0 && b[len(b)-1]&0x80 != 0 {
		b = b[:len(b)-1]
	}
	return string(b)
}

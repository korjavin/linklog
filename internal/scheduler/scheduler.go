package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/korjavin/linklog/internal/bot"
	"github.com/korjavin/linklog/internal/llm"
	"github.com/korjavin/linklog/internal/outline"
	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron        *cron.Cron
	outClient   *outline.Client
	tgBot       *bot.Bot
	llmService  *llm.Service
	scheduleDoc string
	// mu serializes checkSchedule runs so two concurrent invocations (startup
	// goroutine + first cron tick) cannot both read an un-notified entry, both
	// send a notification, and both write back NotifiedAt.
	mu sync.Mutex
}

func NewScheduler(outClient *outline.Client, tgBot *bot.Bot, llmService *llm.Service, scheduleDoc string) *Scheduler {
	return &Scheduler{
		cron:        cron.New(),
		outClient:   outClient,
		tgBot:       tgBot,
		llmService:  llmService,
		scheduleDoc: scheduleDoc,
	}
}

func (s *Scheduler) Start() error {
	if _, err := s.cron.AddFunc("@every 2h", s.checkSchedule); err != nil {
		return fmt.Errorf("failed to schedule job: %w", err)
	}
	s.cron.Start()
	go s.checkSchedule()
	return nil
}

func (s *Scheduler) Stop() {
	s.cron.Stop()
}

func (s *Scheduler) checkSchedule() {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	doc, err := s.outClient.GetDocument(ctx, s.scheduleDoc)
	if err != nil {
		log.Printf("Scheduler: failed to read schedule doc: %v", err)
		return
	}

	today := time.Now().Format("2006-01-02")

	for _, entry := range outline.ParseScheduleTable(doc.Text) {
		if _, err := time.Parse("2006-01-02", entry.Date); err != nil {
			log.Printf("Scheduler: skipping %q with unparseable date %q", entry.Contact, entry.Date)
			continue
		}
		if entry.Date > today {
			continue
		}
		// NotifiedAt >= Date means we already sent a notification for this
		// schedule date. The field is reset implicitly when the user or bot
		// sets a new future Date (making NotifiedAt < Date again).
		if entry.NotifiedAt >= entry.Date {
			continue
		}

		msg := s.buildReminderMessage(entry)
		if err := s.tgBot.NotifyWithButtons(entry.Contact, msg); err != nil {
			// Don't record notifiedAt — let the next tick retry.
			continue
		}

		writeCtx, writeCancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := s.tgBot.SetNotifiedAt(writeCtx, entry.Contact, today); err != nil {
			log.Printf("Scheduler: failed to persist notifiedAt for %q: %v", entry.Contact, err)
		}
		writeCancel()
	}
}

const enrichTimeout = 90 * time.Second

// buildReminderMessage composes the notification text. If the entry has a
// stored topic it is included in the header for a fast preview even before the
// LLM enrichment runs. Falls back to a plain header on LLM errors.
func (s *Scheduler) buildReminderMessage(entry outline.ScheduleEntry) string {
	header := fmt.Sprintf("Reminder: follow up with %s (scheduled %s)", entry.Contact, entry.Date)
	if entry.Topic != "" {
		header += "\nTopic: " + entry.Topic
	}
	if s.llmService == nil {
		return header
	}
	ctx, cancel := context.WithTimeout(context.Background(), enrichTimeout)
	defer cancel()

	summary, err := s.llmService.EnrichReminder(ctx, entry.Contact, entry.Date, entry.Topic)
	if err != nil {
		log.Printf("Scheduler: failed to enrich reminder for %q: %v", entry.Contact, err)
		return header
	}
	if summary == "" {
		return header
	}
	return header + "\n\n" + summary
}

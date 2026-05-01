package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/korjavin/linklog/internal/bot"
	"github.com/korjavin/linklog/internal/outline"
	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron        *cron.Cron
	outClient   *outline.Client
	tgBot       *bot.Bot
	scheduleDoc string

	mu sync.Mutex
	// notifiedOn tracks "contact|date" pairs we already notified for. Keyed on the entry's
	// scheduled date (not today), so updating the date in the doc re-arms the reminder while
	// stale overdue entries don't spam every tick.
	notifiedOn map[string]bool
}

func NewScheduler(outClient *outline.Client, tgBot *bot.Bot, scheduleDoc string) *Scheduler {
	return &Scheduler{
		cron:        cron.New(),
		outClient:   outClient,
		tgBot:       tgBot,
		scheduleDoc: scheduleDoc,
		notifiedOn:  make(map[string]bool),
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	doc, err := s.outClient.GetDocument(ctx, s.scheduleDoc)
	if err != nil {
		log.Printf("Scheduler: failed to read schedule doc: %v", err)
		return
	}

	today := time.Now().Format("2006-01-02")
	for _, entry := range outline.ParseScheduleTable(doc.Text) {
		parsed, err := time.Parse("2006-01-02", entry.Date)
		if err != nil {
			log.Printf("Scheduler: skipping %q with unparseable date %q", entry.Contact, entry.Date)
			continue
		}
		if parsed.Format("2006-01-02") > today {
			continue
		}
		key := entry.Contact + "|" + entry.Date
		s.mu.Lock()
		already := s.notifiedOn[key]
		s.mu.Unlock()
		if already {
			continue
		}
		if err := s.tgBot.Notify(fmt.Sprintf("Reminder: time to follow up with %s (scheduled %s)", entry.Contact, entry.Date)); err != nil {
			// Don't mark as notified — let the next tick retry.
			continue
		}
		s.mu.Lock()
		s.notifiedOn[key] = true
		s.mu.Unlock()
	}
}

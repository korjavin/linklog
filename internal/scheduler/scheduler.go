package scheduler

import (
	"fmt"
	"log"
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
	// notifiedOn tracks "contact|date" strings we already notified for, to avoid spamming each cron tick.
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
	return nil
}

func (s *Scheduler) Stop() {
	s.cron.Stop()
}

func (s *Scheduler) checkSchedule() {
	doc, err := s.outClient.GetDocument(s.scheduleDoc)
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
		key := entry.Contact + "|" + today
		if s.notifiedOn[key] {
			continue
		}
		s.tgBot.Notify(fmt.Sprintf("Reminder: time to follow up with %s (scheduled %s)", entry.Contact, entry.Date))
		s.notifiedOn[key] = true
	}
}

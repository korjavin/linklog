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
}

func NewScheduler(outClient *outline.Client, tgBot *bot.Bot, scheduleDoc string) *Scheduler {
	c := cron.New()
	return &Scheduler{
		cron:        c,
		outClient:   outClient,
		tgBot:       tgBot,
		scheduleDoc: scheduleDoc,
	}
}

func (s *Scheduler) Start() {
	// Run every 2 hours
	_, err := s.cron.AddFunc("@every 2h", s.checkSchedule)
	if err != nil {
		log.Printf("Failed to schedule job: %v", err)
		return
	}
	log.Println("Scheduler started...")
	s.cron.Start()
}

func (s *Scheduler) Stop() {
	log.Println("Scheduler stopping...")
	s.cron.Stop()
}

func (s *Scheduler) checkSchedule() {
	log.Println("Checking schedule table for due interactions...")
	doc, err := s.outClient.GetDocument(s.scheduleDoc)
	if err != nil {
		log.Printf("Failed to get schedule doc: %v", err)
		return
	}

	entries := outline.ParseScheduleTable(doc.Text)
	now := time.Now()

	for _, entry := range entries {
		// Try to parse the date. Format is likely relative or fuzzy as it comes from LLM.
		// We'll use a simple heuristic or expect a specific format like "2006-01-02".
		// Since the plan doesn't specify a strict date parser, we will try standard format.
		parsedDate, err := time.Parse("2006-01-02", entry.Date)
		if err == nil {
			if now.After(parsedDate) || now.Format("2006-01-02") == parsedDate.Format("2006-01-02") {
				msg := fmt.Sprintf("Reminder: It is time to contact %s (Scheduled date: %s)", entry.Contact, entry.Date)
				s.tgBot.Notify(entry.Contact, msg)
			}
		} else {
			// If we can't parse it, we just log it or we might need a better parser.
			// For this task, we can just do a very basic string match or assume LLM output is standard.
			log.Printf("Could not parse date '%s' for contact %s: %v", entry.Date, entry.Contact, err)
		}
	}
}

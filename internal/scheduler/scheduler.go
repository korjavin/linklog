package scheduler

import (
	"context"
	"fmt"
	"log"
	"strings"
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

// notifiedRetention bounds how far back we keep entries in notifiedOn. Once a
// scheduled date is older than this, the user has either acted on it (updating
// the date in the doc, which produces a new key) or it has gone stale — either
// way, the old notification record is no longer load-bearing and would only
// grow the map without bound.
const notifiedRetention = 90 * 24 * time.Hour

func (s *Scheduler) checkSchedule() {
	// Hold the lock for the entire run. The function is invoked at most every
	// 2h by cron plus once at startup; serializing the body removes the
	// read-then-write race on notifiedOn (where two concurrent runs could each
	// observe `already == false` and double-notify) and lets us prune the map
	// safely in the same critical section.
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	doc, err := s.outClient.GetDocument(ctx, s.scheduleDoc)
	if err != nil {
		log.Printf("Scheduler: failed to read schedule doc: %v", err)
		return
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	s.pruneNotifiedLocked(now)

	for _, entry := range outline.ParseScheduleTable(doc.Text) {
		if _, err := time.Parse("2006-01-02", entry.Date); err != nil {
			log.Printf("Scheduler: skipping %q with unparseable date %q", entry.Contact, entry.Date)
			continue
		}
		// ISO-8601 dates sort lexically, so a string compare is equivalent to a
		// time compare here and avoids a redundant Format round-trip.
		if entry.Date > today {
			continue
		}
		key := entry.Contact + "|" + entry.Date
		if s.notifiedOn[key] {
			continue
		}
		if err := s.tgBot.Notify(fmt.Sprintf("Reminder: time to follow up with %s (scheduled %s)", entry.Contact, entry.Date)); err != nil {
			// Don't mark as notified — let the next tick retry.
			continue
		}
		s.notifiedOn[key] = true
	}
}

// pruneNotifiedLocked drops notification records whose date is older than
// notifiedRetention. Caller must hold s.mu.
func (s *Scheduler) pruneNotifiedLocked(now time.Time) {
	cutoff := now.Add(-notifiedRetention)
	for key := range s.notifiedOn {
		// key format: "contact|YYYY-MM-DD"
		idx := strings.LastIndex(key, "|")
		if idx < 0 {
			continue
		}
		dateStr := key[idx+1:]
		parsed, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			delete(s.notifiedOn, key)
			continue
		}
		if parsed.Before(cutoff) {
			delete(s.notifiedOn, key)
		}
	}
}

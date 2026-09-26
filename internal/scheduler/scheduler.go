package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/Thiht/pici/internal/cron"
	"github.com/Thiht/pici/internal/stores"
)

type Scheduler struct {
	Store    stores.Store
	Trigger  func(ctx context.Context, project stores.Project, workflow, ref string, trigger stores.Trigger) error
	Interval time.Duration
}

func (s *Scheduler) Run(ctx context.Context) {
	if s.Interval <= 0 {
		s.Interval = time.Minute
	}
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	due, err := s.Store.ListDueSchedules(ctx, time.Now())
	if err != nil {
		log.Printf("scheduler: list due schedules: %v", err)
		return
	}
	for _, sch := range due {
		project, err := s.Store.GetProject(ctx, sch.ProjectID)
		if err != nil {
			continue
		}
		ref := project.DefaultBranch
		if err := s.Trigger(ctx, project, sch.Workflow, ref, stores.TriggerCron); err != nil {
			log.Printf("scheduler: trigger %s/%s: %v", project.Name, sch.Workflow, err)
			continue
		}
		if next, err := cron.Next(sch.CronExpr, time.Now()); err == nil {
			sch.NextRunAt = next
			_ = s.Store.UpsertSchedule(ctx, sch)
		}
	}
}

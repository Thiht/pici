package ci

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/Thiht/pici/internal/cron"
	"github.com/Thiht/pici/internal/stores"
)

func SyncSchedule(ctx context.Context, store stores.Store, projectID, workflow, cronExpr string) error {
	if cronExpr == "" {
		return store.DeleteSchedule(ctx, projectID, workflow)
	}
	next, err := cron.Next(cronExpr, time.Now())
	if err != nil {
		return err
	}
	return store.UpsertSchedule(ctx, stores.Schedule{
		ProjectID: projectID,
		Workflow:  workflow,
		CronExpr:  cronExpr,
		NextRunAt: next.UnixMilli(),
	})
}

func SyncProjectSchedules(ctx context.Context, store stores.Store, projectID, cloneDir string) error {
	ciDir := filepath.Join(cloneDir, ".ci")
	entries, err := os.ReadDir(ciDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(ciDir, e.Name(), "ci.yml"))
		if err != nil {
			continue
		}
		cfg, err := Parse(data)
		if err != nil {
			continue
		}
		if err := SyncSchedule(ctx, store, projectID, e.Name(), cfg.Schedule); err != nil {
			return err
		}
	}
	return nil
}

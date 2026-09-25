package gc

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/stores"
)

type Collector struct {
	Store        stores.Store
	Engine       *docker.Engine
	WorkspaceDir string
	LogsDir      string
	Keep         time.Duration
	Interval     time.Duration
}

func (c *Collector) Run(ctx context.Context) {
	if c.Interval <= 0 {
		c.Interval = 10 * time.Minute
	}
	if c.Keep <= 0 {
		c.Keep = 24 * time.Hour
	}
	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

func (c *Collector) tick(ctx context.Context) {
	c.cleanupWorkspaces(ctx)
	c.cleanupLogs(ctx)
	if c.Engine != nil {
		if err := c.Engine.PruneImages(ctx); err != nil {
			log.Printf("gc: prune images: %v", err)
		}
	}
}

func (c *Collector) cleanupWorkspaces(ctx context.Context) {
	cutoff := time.Now().Add(-c.Keep).UnixMilli()
	projects, err := os.ReadDir(c.WorkspaceDir)
	if err != nil {
		return
	}
	for _, proj := range projects {
		if !proj.IsDir() {
			continue
		}
		projPath := filepath.Join(c.WorkspaceDir, proj.Name())
		execs, err := os.ReadDir(projPath)
		if err != nil {
			continue
		}
		for _, e := range execs {
			if !e.IsDir() {
				continue
			}
			if e.Name() == "_discovery" {
				if olderThan(filepath.Join(projPath, e.Name()), c.Keep) {
					_ = os.RemoveAll(filepath.Join(projPath, e.Name()))
				}
				continue
			}
			if c.expired(ctx, e.Name(), cutoff) {
				_ = os.RemoveAll(filepath.Join(projPath, e.Name()))
			}
		}
	}
}

func (c *Collector) cleanupLogs(ctx context.Context) {
	cutoff := time.Now().Add(-c.Keep).UnixMilli()
	entries, err := os.ReadDir(c.LogsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if c.expired(ctx, e.Name(), cutoff) {
			_ = os.RemoveAll(filepath.Join(c.LogsDir, e.Name()))
		}
	}
}

func (c *Collector) expired(ctx context.Context, execID string, cutoff int64) bool {
	exec, err := c.Store.GetExecution(ctx, execID)
	if err != nil {
		return true
	}
	switch exec.Status {
	case stores.StatusSuccess, stores.StatusFailed, stores.StatusCanceled:
		return exec.FinishedAt > 0 && exec.FinishedAt < cutoff
	default:
		return false
	}
}

func olderThan(path string, d time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) > d
}

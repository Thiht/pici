package gc

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/stores"
)

type Engine interface {
	PruneImages(ctx context.Context) error
	Sandboxes(ctx context.Context) ([]docker.SandboxRef, error)
	RemoveSandbox(ctx context.Context, id string) error
}

// Registry is the shared image cache the collector keeps under its size cap.
type Registry interface {
	RegistrySize(ctx context.Context) (int64, error)
	PurgeRegistry(ctx context.Context) error
}

type Collector struct {
	Store                stores.Store
	Engine               Engine
	Registry             Registry
	WorkspaceDir         string
	LogsDir              string
	Keep                 time.Duration
	Interval             time.Duration
	RegistryCacheMaxSize int64
}

func (c *Collector) Run(ctx context.Context) {
	if c.Interval <= 0 {
		c.Interval = 10 * time.Minute
	}
	if c.Keep <= 0 {
		c.Keep = 24 * time.Hour
	}
	c.tick(ctx)
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
	c.cleanupUploads()
	if c.Engine != nil {
		c.cleanupSandboxes(ctx)
		if err := c.Engine.PruneImages(ctx); err != nil {
			slog.WarnContext(ctx, "prune images", "error", err)
		}
	}
	c.cleanupRegistryCache(ctx)
}

// cleanupSandboxes removes the sandboxes of executions that are no longer
// running: terminal ones, and requeued ones whose attempt went back to pending
// after a pici restart.
func (c *Collector) cleanupSandboxes(ctx context.Context) {
	sandboxes, err := c.Engine.Sandboxes(ctx)
	if err != nil {
		return
	}
	for _, sb := range sandboxes {
		projectID, err := uuid.Parse(sb.ProjectID)
		if err != nil {
			continue
		}
		exec, err := c.Store.GetAttempt(ctx, projectID, sb.Attempt)
		if errors.Is(err, stores.ErrNotFound) {
			c.reapSandbox(ctx, sb, "execution no longer exists")
			continue
		}
		if err != nil || exec.Status == stores.StatusRunning {
			continue
		}
		c.reapSandbox(ctx, sb, "execution is "+string(exec.Status))
	}
}

func (c *Collector) reapSandbox(ctx context.Context, sb docker.SandboxRef, reason string) {
	slog.WarnContext(ctx, "removing orphaned sandbox", "id", sb.ID, "project", sb.ProjectID, "attempt", sb.Attempt, "reason", reason)
	if err := c.Engine.RemoveSandbox(ctx, sb.ID); err != nil {
		slog.WarnContext(ctx, "remove orphaned sandbox", "id", sb.ID, "error", err)
	}
}

// cleanupRegistryCache purges the image cache once it grows past its cap.
func (c *Collector) cleanupRegistryCache(ctx context.Context) {
	if c.Registry == nil || c.RegistryCacheMaxSize <= 0 {
		return
	}
	usage, err := c.Registry.RegistrySize(ctx)
	if err != nil || usage <= c.RegistryCacheMaxSize {
		return
	}
	slog.WarnContext(ctx, "purging image cache over its size cap", "size", usage, "max", c.RegistryCacheMaxSize)
	if err := c.Registry.PurgeRegistry(ctx); err != nil {
		slog.WarnContext(ctx, "purge image cache", "error", err)
	}
}

func (c *Collector) cleanupWorkspaces(ctx context.Context) {
	cutoff := time.Now().Add(-c.Keep)
	projects, err := os.ReadDir(c.WorkspaceDir)
	if err != nil {
		return
	}
	for _, proj := range projects {
		if !proj.IsDir() {
			continue
		}
		projectID, err := uuid.Parse(proj.Name())
		if err != nil {
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
			id, err := strconv.ParseInt(e.Name(), 10, 64)
			if err != nil {
				_ = os.RemoveAll(filepath.Join(projPath, e.Name()))
				continue
			}
			retained, err := c.Store.WorkspaceRetained(ctx, projectID, id, cutoff)
			if err != nil || !retained {
				_ = os.RemoveAll(filepath.Join(projPath, e.Name()))
			}
		}
	}
}

func (c *Collector) cleanupLogs(ctx context.Context) {
	cutoff := time.Now().Add(-c.Keep)
	projects, err := os.ReadDir(c.LogsDir)
	if err != nil {
		return
	}
	for _, proj := range projects {
		if !proj.IsDir() {
			continue
		}
		projectID, err := uuid.Parse(proj.Name())
		if err != nil {
			continue
		}
		projPath := filepath.Join(c.LogsDir, proj.Name())
		execs, err := os.ReadDir(projPath)
		if err != nil {
			continue
		}
		for _, e := range execs {
			if !e.IsDir() {
				continue
			}
			id, err := strconv.ParseInt(e.Name(), 10, 64)
			if err != nil || c.expired(ctx, projectID, id, cutoff) {
				_ = os.RemoveAll(filepath.Join(projPath, e.Name()))
			}
		}
	}
}

func (c *Collector) cleanupUploads() {
	root := filepath.Join(c.WorkspaceDir, "uploads")
	projects, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, proj := range projects {
		if !proj.IsDir() {
			continue
		}
		projPath := filepath.Join(root, proj.Name())
		files, err := os.ReadDir(projPath)
		if err != nil {
			continue
		}
		for _, f := range files {
			path := filepath.Join(projPath, f.Name())
			if olderThan(path, c.Keep) {
				_ = os.RemoveAll(path)
			}
		}
	}
}

func (c *Collector) expired(ctx context.Context, projectID uuid.UUID, id int64, cutoff time.Time) bool {
	exec, err := c.Store.GetAttempt(ctx, projectID, id)
	if err != nil {
		return true
	}
	switch exec.Status {
	case stores.StatusSuccess, stores.StatusFailed, stores.StatusCanceled:
		return exec.FinishedAt != nil && exec.FinishedAt.Before(cutoff)
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

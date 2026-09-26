package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/config"
	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/gc"
	"github.com/Thiht/pici/internal/handlers"
	"github.com/Thiht/pici/internal/scheduler"
	"github.com/Thiht/pici/internal/secrets"
	"github.com/Thiht/pici/internal/stores"
)

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	cipher, err := secrets.New(cfg.SecretKey)
	if err != nil {
		slog.Error("load secret key", "error", err)
		os.Exit(1)
	}

	store, err := stores.Open(cfg.DBDriver, cfg.DBDSN, cipher)
	if err != nil {
		slog.Error("open store", "error", err)
		os.Exit(1)
	}
	defer func() { _ = store.Close() }()

	if err := store.RequeueOrphanedExecutions(context.Background()); err != nil {
		slog.Warn("requeue orphaned executions", "error", err)
	}

	engine, err := docker.New()
	if err != nil {
		slog.Warn("docker engine unavailable", "error", err)
	}

	if err := os.MkdirAll(cfg.WorkspaceDir, 0o755); err != nil {
		slog.Error("create workspace", "error", err)
		os.Exit(1)
	}

	runner := &ci.Runner{
		Store:              store,
		Engine:             engine,
		WorkspaceDir:       cfg.WorkspaceDir,
		MountPath:          cfg.RepoMountPath,
		LogsDir:            filepath.Join(cfg.WorkspaceDir, "logs"),
		DefaultStepTimeout: cfg.StepTimeout,
		PublicBaseURL:      cfg.PublicBaseURL,
	}

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	runner.Start(cfg.Concurrency)

	sch := &scheduler.Scheduler{
		Store:    store,
		Interval: cfg.SchedulerInterval,
		Trigger: func(ctx context.Context, p stores.Project, workflow, ref string, trigger stores.Trigger) error {
			_, err := runner.Enqueue(ctx, p, workflow, ref, "", trigger)
			return err
		},
	}
	go sch.Run(rootCtx)

	collector := &gc.Collector{
		Store:        store,
		Engine:       engine,
		WorkspaceDir: cfg.WorkspaceDir,
		LogsDir:      filepath.Join(cfg.WorkspaceDir, "logs"),
		Keep:         cfg.GCKeepDuration,
		Interval:     cfg.GCInterval,
	}
	go collector.Run(rootCtx)

	handler := handlers.New(store, runner, engine, cfg.WorkspaceDir, cfg.RepoMountPath, cfg.APIToken)

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: handler.Routes(),
	}

	go func() {
		slog.Info("listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("shutting down")

	rootCancel()

	httpCtx, httpCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer httpCancel()
	_ = httpServer.Shutdown(httpCtx)

	drainCtx, drainCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer drainCancel()
	runner.Drain(drainCtx)
}

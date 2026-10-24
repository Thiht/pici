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
	"strings"
	"syscall"
	"time"

	"github.com/Thiht/pici/internal/app/artifacts"
	"github.com/Thiht/pici/internal/app/cache"
	"github.com/Thiht/pici/internal/app/executions"
	"github.com/Thiht/pici/internal/app/projects"
	"github.com/Thiht/pici/internal/app/variables"
	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/config"
	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/gc"
	"github.com/Thiht/pici/internal/handlers"
	"github.com/Thiht/pici/internal/logging"
	"github.com/Thiht/pici/internal/scheduler"
	"github.com/Thiht/pici/internal/secrets"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/web"
	"github.com/Thiht/pici/internal/workspace"
)

var buildVersion = "dev"

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	logging.Setup(cfg.LogFormat)

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

	var registries docker.Registries
	if engine != nil {
		registryCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		registries, err = engine.EnsureRegistries(registryCtx)
		cancel()
		if err != nil {
			slog.Warn("start image registries", "error", err)
		}
	}

	if err := os.MkdirAll(cfg.WorkspaceDir, 0o755); err != nil {
		slog.Error("create workspace", "error", err)
		os.Exit(1)
	}

	ws := workspace.New(cfg.WorkspaceDir, filepath.Join(cfg.WorkspaceDir, "logs"))

	var runnerEngine ci.Engine
	if engine != nil {
		runnerEngine = engine
	}
	runner := &ci.Runner{
		Store:              store,
		Engine:             runnerEngine,
		Workspace:          ws,
		MountPath:          cfg.RepoMountPath,
		DefaultStepTimeout: cfg.StepTimeout,
		MaxSnapshotSize:    cfg.MaxSnapshotSize,
		PublicBaseURL:      cfg.PublicBaseURL,
		SandboxImage:       cfg.SandboxImage,
		SandboxAddr:        cfg.SandboxAddr,
		Registries:         registries,
	}

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	runner.Start(cfg.Concurrency)

	var provider auth.Provider = auth.NewGitHubProvider(
		cfg.GitHubClientID,
		cfg.GitHubClientSecret,
		cfg.PublicBaseURL+"/auth/github/callback",
		len(cfg.GitHubTeams) > 0,
	)
	accessTeams, accessUsers := cfg.GitHubTeams, cfg.GitHubUsers
	switch cfg.AuthProvider {
	case "gitlab":
		provider = auth.NewGitLabProvider(
			cfg.GitLabURL,
			cfg.GitLabClientID,
			cfg.GitLabClientSecret,
			cfg.PublicBaseURL+"/auth/gitlab/callback",
		)
		accessTeams, accessUsers = cfg.GitLabGroups, cfg.GitLabUsers
	case "oidc":
		oidcProvider, err := auth.NewOIDCProvider(
			rootCtx,
			cfg.OIDCIssuer,
			cfg.OIDCClientID,
			cfg.OIDCClientSecret,
			cfg.PublicBaseURL+"/auth/oidc/callback",
			cfg.OIDCDisplayName,
			cfg.OIDCScopes,
		)
		if err != nil {
			slog.Error("discover OIDC provider", "error", err)
			os.Exit(1)
		}
		provider = oidcProvider
		accessTeams, accessUsers = cfg.OIDCGroups, cfg.OIDCUsers
	}

	authSvc := auth.NewService(
		store,
		provider,
		accessTeams,
		accessUsers,
		cfg.SessionTTL,
		strings.HasPrefix(strings.ToLower(cfg.PublicBaseURL), "https://"),
	)
	go authSvc.RunRevalidator(rootCtx, time.Hour)

	sch := &scheduler.Scheduler{
		Store:    store,
		Interval: cfg.SchedulerInterval,
		Trigger: func(ctx context.Context, p stores.Project, workflow, ref string, trigger stores.Trigger) error {
			_, err := runner.Enqueue(ctx, p, workflow, ref, "", trigger, nil)
			return err
		},
	}
	go sch.Run(rootCtx)

	collector := &gc.Collector{
		Store:                store,
		Engine:               engine,
		WorkspaceDir:         cfg.WorkspaceDir,
		LogsDir:              filepath.Join(cfg.WorkspaceDir, "logs"),
		Keep:                 cfg.GCKeepDuration,
		Interval:             cfg.GCInterval,
		RegistryCacheMaxSize: cfg.RegistryCacheMaxSize,
	}
	go collector.Run(rootCtx)

	var cacheEngine cache.Engine
	if engine != nil {
		cacheEngine = engine
	}

	appProjects := projects.New(store)
	appExecutions := executions.New(store, ws, ws, runner)
	appArtifacts := artifacts.New(ws)
	appCache := cache.New(cacheEngine)
	appVariables := variables.New(store)

	apiHandler := handlers.New(handlers.Deps{
		Store:           store,
		Runner:          runner,
		Engine:          engine,
		Workspace:       ws,
		Projects:        appProjects,
		Executions:      appExecutions,
		Artifacts:       appArtifacts,
		Cache:           appCache,
		Variables:       appVariables,
		MaxSnapshotSize: cfg.MaxSnapshotSize,
		Auth:            authSvc,
		BuildVersion:    buildVersion,
	}).Routes()
	webHandler := web.New(web.Deps{
		Store:         store,
		Runner:        runner,
		Workspace:     ws,
		Projects:      appProjects,
		Executions:    appExecutions,
		Artifacts:     appArtifacts,
		Cache:         appCache,
		Variables:     appVariables,
		Auth:          authSvc,
		PublicBaseURL: cfg.PublicBaseURL,
		BuildVersion:  buildVersion,
	}).Routes()

	root := http.NewServeMux()
	root.Handle("/", webHandler)
	root.Handle("/api/", apiHandler)
	root.Handle("/badge/", apiHandler)
	root.Handle("/health", apiHandler)
	root.Handle("/version", apiHandler)

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: root,
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

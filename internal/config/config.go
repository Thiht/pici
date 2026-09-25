package config

import (
	"errors"
	"flag"
	"os"
	"time"

	"github.com/peterbourgon/ff/v3"
)

type Config struct {
	HTTPAddr      string
	DBDriver      string
	DBDSN         string
	WorkspaceDir  string
	RepoMountPath string
	Concurrency   int
	StepTimeout   time.Duration
	ConfigFile    string

	SecretKey         string
	APIToken          string
	PublicBaseURL     string
	GCInterval        time.Duration
	GCKeepDuration    time.Duration
	SchedulerInterval time.Duration
	ShutdownTimeout   time.Duration
}

func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("pici", flag.ContinueOnError)
	var cfg Config

	fs.StringVar(&cfg.HTTPAddr, "http-addr", ":8080", "HTTP listen address")
	fs.StringVar(&cfg.DBDriver, "db-driver", "sqlite", "database driver (sqlite|postgres)")
	fs.StringVar(&cfg.DBDSN, "db-dsn", "pici.db", "database DSN (path for sqlite, connection string for postgres)")
	fs.StringVar(&cfg.WorkspaceDir, "workspace-dir", defaultWorkspaceDir(), "directory for clones and logs")
	fs.StringVar(&cfg.RepoMountPath, "repo-mount-path", "/workspace", "mount path of the repo inside the runner container")
	fs.IntVar(&cfg.Concurrency, "concurrency", 4, "maximum concurrent executions")
	fs.DurationVar(&cfg.StepTimeout, "step-timeout", 30*time.Minute, "default step timeout")
	fs.StringVar(&cfg.ConfigFile, "config", "", "path to a JSON config file")

	fs.StringVar(&cfg.SecretKey, "secret-key", "", "32-byte key (hex or base64) used to encrypt secrets at rest (required)")
	fs.StringVar(&cfg.APIToken, "api-token", "", "API token required to call the API (required)")
	fs.StringVar(&cfg.PublicBaseURL, "public-url", "", "public base URL used to build links in PR statuses")
	fs.DurationVar(&cfg.GCInterval, "gc-interval", 10*time.Minute, "garbage collection interval")
	fs.DurationVar(&cfg.GCKeepDuration, "gc-keep", 24*time.Hour, "how long to keep finished workspaces and logs")
	fs.DurationVar(&cfg.SchedulerInterval, "scheduler-interval", time.Minute, "scheduled build polling interval")
	fs.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", 30*time.Second, "grace period for in-flight steps on shutdown")

	if err := ff.Parse(fs, args,
		ff.WithEnvVarPrefix("PICI"),
		ff.WithConfigFileFlag("config"),
		ff.WithConfigFileParser(ff.JSONParser),
	); err != nil {
		return Config{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.SecretKey == "" {
		return errors.New("PICI_SECRET_KEY is required (generate one with `openssl rand -hex 32`)")
	}
	if c.APIToken == "" {
		return errors.New("PICI_API_TOKEN is required (generate one with `openssl rand -hex 24`)")
	}
	return nil
}

func defaultWorkspaceDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home + "/.pici/workspaces"
}

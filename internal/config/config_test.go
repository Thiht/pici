package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		SecretKey:          "x",
		PublicBaseURL:      "https://pici.example.com",
		GitHubClientID:     "id",
		GitHubClientSecret: "secret",
		GitHubTeams:        []string{"acme/pici-users"},
		SessionTTL:         1,
		MaxSnapshotSize:    1,
	}
}

func TestLoadPriority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pici.json")
	content := `{
  "http-addr": ":1111",
  "public-url": "https://config.example.com",
  "github-client-id": "config-id",
  "github-client-secret": "config-secret",
  "github-teams": "acme/config",
  "secret-key": "x",
  "session-ttl": "1h",
  "max-snapshot-size": 1
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PICI_HTTP_ADDR", ":2222")
	t.Setenv("PICI_GITHUB_CLIENT_ID", "env-id")

	cfg, err := Load([]string{"-config", path, "-github-client-id", "flag-id"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":2222" {
		t.Fatalf("env should override the config file, got %q", cfg.HTTPAddr)
	}
	if cfg.GitHubClientID != "flag-id" {
		t.Fatalf("flag should override env and the config file, got %q", cfg.GitHubClientID)
	}
	if cfg.GitHubClientSecret != "config-secret" {
		t.Fatalf("config file value not applied, got %q", cfg.GitHubClientSecret)
	}
	if len(cfg.GitHubTeams) != 1 || cfg.GitHubTeams[0] != "acme/config" {
		t.Fatalf("unexpected teams: %#v", cfg.GitHubTeams)
	}
	if cfg.SessionTTL != time.Hour {
		t.Fatalf("unexpected session TTL: %v", cfg.SessionTTL)
	}
}

func TestValidateOK(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateUsersOnly(t *testing.T) {
	cfg := validConfig()
	cfg.GitHubTeams = nil
	cfg.GitHubUsers = []string{"octocat"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateMissingUsersAndTeams(t *testing.T) {
	cfg := validConfig()
	cfg.GitHubTeams = nil
	cfg.GitHubUsers = nil
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error when neither users nor teams are configured")
	}
}

func TestValidateInvalidUser(t *testing.T) {
	for _, user := range []string{"", "acme/user", "octo cat", "octo\tcat"} {
		cfg := validConfig()
		cfg.GitHubTeams = nil
		cfg.GitHubUsers = []string{user}
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected error for user %q", user)
		}
	}
}

func TestValidateLocalhostAllowsHTTP(t *testing.T) {
	cfg := validConfig()
	cfg.PublicBaseURL = "http://localhost:8080"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateMissingSecretKey(t *testing.T) {
	cfg := validConfig()
	cfg.SecretKey = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing secret key")
	}
}

func TestValidatePublicURL(t *testing.T) {
	cases := map[string]string{
		"missing":    "",
		"invalid":    "not a url",
		"http":       "http://pici.example.com",
		"no scheme":  "pici.example.com",
		"with path":  "https://pici.example.com/pici",
		"with query": "https://pici.example.com?x=1",
	}
	for name, value := range cases {
		cfg := validConfig()
		cfg.PublicBaseURL = value
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected error for public URL %q", name, value)
		}
	}
}

func TestValidateMissingGitHubConfig(t *testing.T) {
	cases := map[string]func(*Config){
		"client id":     func(c *Config) { c.GitHubClientID = "" },
		"client secret": func(c *Config) { c.GitHubClientSecret = "" },
		"teams":         func(c *Config) { c.GitHubTeams = nil },
	}
	for name, mutate := range cases {
		cfg := validConfig()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestValidateInvalidTeam(t *testing.T) {
	for _, team := range []string{"acme", "acme/", "/team", "a/b/c", "acme / team"} {
		cfg := validConfig()
		cfg.GitHubTeams = []string{team}
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected error for team %q", team)
		}
	}
}

func TestValidateInvalidSessionTTL(t *testing.T) {
	for _, ttl := range []int64{0, -1} {
		cfg := validConfig()
		cfg.SessionTTL = time.Duration(ttl)
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected error for session TTL %d", ttl)
		}
	}
}

func TestValidateInvalidMaxSnapshotSize(t *testing.T) {
	for _, size := range []int64{0, -1} {
		cfg := validConfig()
		cfg.MaxSnapshotSize = size
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for max snapshot size %d", size)
		}
	}
}

func TestValidateRepoMountPathOverlapsCache(t *testing.T) {
	for _, path := range []string{"/", "/pici", "/pici/cache", "/pici/cache/nested"} {
		cfg := validConfig()
		cfg.RepoMountPath = path
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for repo mount path %q", path)
		}
	}
}

func TestValidateRepoMountPathOK(t *testing.T) {
	for _, path := range []string{"", "/workspace", "/workspace/nested", "/pici2"} {
		cfg := validConfig()
		cfg.RepoMountPath = path
		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error for repo mount path %q: %v", path, err)
		}
	}
}

func TestSplitList(t *testing.T) {
	got := splitList(" acme/a, ,other/b ")
	if len(got) != 2 || got[0] != "acme/a" || got[1] != "other/b" {
		t.Fatalf("splitList = %#v", got)
	}
}

package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		SecretKey:          "x",
		PublicBaseURL:      "https://pici.example.com",
		AuthProvider:       "github",
		GitHubClientID:     "id",
		GitHubClientSecret: "secret",
		GitHubTeams:        []string{"acme/pici-users"},
		SessionTTL:         1,
		MaxSnapshotSize:    1,
	}
}

func validGitLabConfig() Config {
	return Config{
		SecretKey:          "x",
		PublicBaseURL:      "https://pici.example.com",
		AuthProvider:       "gitlab",
		GitLabURL:          "https://gitlab.example.com",
		GitLabClientID:     "id",
		GitLabClientSecret: "secret",
		GitLabGroups:       []string{"acme/pici-users"},
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
	if cfg.AuthProvider != "github" {
		t.Fatalf("unexpected default auth provider: %q", cfg.AuthProvider)
	}
}

func TestLoadGitLabProvider(t *testing.T) {
	t.Setenv("PICI_AUTH_PROVIDER", " GitLab ")
	t.Setenv("PICI_GITLAB_URL", "https://gitlab.example.com/")
	t.Setenv("PICI_GITLAB_CLIENT_ID", "id")
	t.Setenv("PICI_GITLAB_CLIENT_SECRET", "secret")
	t.Setenv("PICI_GITLAB_GROUPS", "acme/pici, acme/platform")
	t.Setenv("PICI_GITLAB_USERS", "alice")
	t.Setenv("PICI_SECRET_KEY", "x")
	t.Setenv("PICI_PUBLIC_URL", "https://pici.example.com")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthProvider != "gitlab" {
		t.Fatalf("unexpected provider: %q", cfg.AuthProvider)
	}
	if cfg.GitLabURL != "https://gitlab.example.com" {
		t.Fatalf("unexpected GitLab URL: %q", cfg.GitLabURL)
	}
	if len(cfg.GitLabGroups) != 2 || cfg.GitLabGroups[0] != "acme/pici" || cfg.GitLabGroups[1] != "acme/platform" {
		t.Fatalf("unexpected groups: %#v", cfg.GitLabGroups)
	}
	if len(cfg.GitLabUsers) != 1 || cfg.GitLabUsers[0] != "alice" {
		t.Fatalf("unexpected users: %#v", cfg.GitLabUsers)
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

func TestLoadOIDCProvider(t *testing.T) {
	t.Setenv("PICI_AUTH_PROVIDER", "OIDC")
	t.Setenv("PICI_OIDC_ISSUER", "https://keycloak.example.com/realms/pici")
	t.Setenv("PICI_OIDC_CLIENT_ID", "id")
	t.Setenv("PICI_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("PICI_OIDC_GROUPS", "acme/pici, acme/platform")
	t.Setenv("PICI_OIDC_USERS", "alice")
	t.Setenv("PICI_OIDC_SCOPES", "openid, profile,email")
	t.Setenv("PICI_OIDC_NAME", "Keycloak")
	t.Setenv("PICI_SECRET_KEY", "x")
	t.Setenv("PICI_PUBLIC_URL", "https://pici.example.com")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthProvider != "oidc" || cfg.OIDCDisplayName != "Keycloak" {
		t.Fatalf("unexpected OIDC config: %+v", cfg)
	}
	if len(cfg.OIDCGroups) != 2 || len(cfg.OIDCUsers) != 1 {
		t.Fatalf("unexpected access lists: %#v %#v", cfg.OIDCGroups, cfg.OIDCUsers)
	}
	want := []string{"openid", "profile", "email"}
	if !slices.Equal(cfg.OIDCScopes, want) {
		t.Fatalf("scopes = %#v, want %#v", cfg.OIDCScopes, want)
	}
}

func TestLoadOIDCDefaultScopes(t *testing.T) {
	t.Setenv("PICI_AUTH_PROVIDER", "oidc")
	t.Setenv("PICI_OIDC_ISSUER", "https://keycloak.example.com/realms/pici")
	t.Setenv("PICI_OIDC_CLIENT_ID", "id")
	t.Setenv("PICI_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("PICI_OIDC_USERS", "alice")
	t.Setenv("PICI_SECRET_KEY", "x")
	t.Setenv("PICI_PUBLIC_URL", "https://pici.example.com")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"openid", "profile", "email", "offline_access"}
	if !slices.Equal(cfg.OIDCScopes, want) {
		t.Fatalf("scopes = %#v, want %#v", cfg.OIDCScopes, want)
	}
}

func TestValidateGitLabOK(t *testing.T) {
	if err := validGitLabConfig().Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateGitLabUsersOnly(t *testing.T) {
	cfg := validGitLabConfig()
	cfg.GitLabGroups = nil
	cfg.GitLabUsers = []string{"alice"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateGitLabMissingAccessList(t *testing.T) {
	cfg := validGitLabConfig()
	cfg.GitLabGroups = nil
	cfg.GitLabUsers = nil
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error when neither users nor groups are configured")
	}
}

func TestValidateGitLabMissingCredentials(t *testing.T) {
	cases := map[string]func(*Config){
		"client id":     func(c *Config) { c.GitLabClientID = "" },
		"client secret": func(c *Config) { c.GitLabClientSecret = "" },
	}
	for name, mutate := range cases {
		cfg := validGitLabConfig()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestValidateGitLabURL(t *testing.T) {
	for _, value := range []string{"", "not a url", "ftp://gitlab.example.com", "https://", "https://gitlab.example.com?x=1"} {
		cfg := validGitLabConfig()
		cfg.GitLabURL = value
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected error for GitLab URL %q", value)
		}
	}
	for _, value := range []string{"https://gitlab.example.com", "http://localhost:8081", "https://example.com/gitlab"} {
		cfg := validGitLabConfig()
		cfg.GitLabURL = value
		if err := cfg.Validate(); err != nil {
			t.Errorf("unexpected error for GitLab URL %q: %v", value, err)
		}
	}
}

func TestValidateGitLabGroup(t *testing.T) {
	for _, group := range []string{"", "/acme", "acme/", "a//b", "acme / pici", "a/b c"} {
		cfg := validGitLabConfig()
		cfg.GitLabGroups = []string{group}
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected error for GitLab group %q", group)
		}
	}
	for _, group := range []string{"acme", "acme/pici", "acme/sub/pici"} {
		cfg := validGitLabConfig()
		cfg.GitLabGroups = []string{group}
		if err := cfg.Validate(); err != nil {
			t.Errorf("unexpected error for GitLab group %q: %v", group, err)
		}
	}
}

func validOIDCConfig() Config {
	return Config{
		SecretKey:        "x",
		PublicBaseURL:    "https://pici.example.com",
		AuthProvider:     "oidc",
		OIDCIssuer:       "https://keycloak.example.com/realms/pici",
		OIDCClientID:     "id",
		OIDCClientSecret: "secret",
		OIDCGroups:       []string{"acme/pici-users"},
		OIDCScopes:       []string{"openid", "profile", "email"},
		OIDCDisplayName:  "Keycloak",
		SessionTTL:       1,
		MaxSnapshotSize:  1,
	}
}

func TestValidateOIDCOK(t *testing.T) {
	if err := validOIDCConfig().Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateOIDCUsersOnly(t *testing.T) {
	cfg := validOIDCConfig()
	cfg.OIDCGroups = nil
	cfg.OIDCUsers = []string{"alice", "alice@example.com"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateOIDCMissingAccessList(t *testing.T) {
	cfg := validOIDCConfig()
	cfg.OIDCGroups = nil
	cfg.OIDCUsers = nil
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error when neither users nor groups are configured")
	}
}

func TestValidateOIDCMissingCredentials(t *testing.T) {
	cases := map[string]func(*Config){
		"issuer":        func(c *Config) { c.OIDCIssuer = "" },
		"client id":     func(c *Config) { c.OIDCClientID = "" },
		"client secret": func(c *Config) { c.OIDCClientSecret = "" },
	}
	for name, mutate := range cases {
		cfg := validOIDCConfig()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestValidateOIDCIssuerURL(t *testing.T) {
	for _, value := range []string{"not a url", "ftp://keycloak.example.com", "https://", "https://keycloak.example.com?x=1"} {
		cfg := validOIDCConfig()
		cfg.OIDCIssuer = value
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected error for OIDC issuer %q", value)
		}
	}
	for _, value := range []string{"https://keycloak.example.com/realms/pici", "http://localhost:8081/realms/pici"} {
		cfg := validOIDCConfig()
		cfg.OIDCIssuer = value
		if err := cfg.Validate(); err != nil {
			t.Errorf("unexpected error for OIDC issuer %q: %v", value, err)
		}
	}
}

func TestValidateOIDCScopes(t *testing.T) {
	cfg := validOIDCConfig()
	cfg.OIDCScopes = []string{"profile", "email"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an error when the openid scope is missing")
	}
}

func TestValidateOIDCInvalidUser(t *testing.T) {
	for _, user := range []string{"", "alice/bob", "ali ce"} {
		cfg := validOIDCConfig()
		cfg.OIDCGroups = nil
		cfg.OIDCUsers = []string{user}
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected error for OIDC user %q", user)
		}
	}
}

func TestValidateGitLabIgnoresGitHubConfig(t *testing.T) {
	cfg := validGitLabConfig()
	cfg.GitHubClientID = ""
	cfg.GitHubClientSecret = ""
	cfg.GitHubTeams = nil
	cfg.GitHubUsers = nil
	if err := cfg.Validate(); err != nil {
		t.Fatalf("GitHub settings must be ignored in GitLab mode: %v", err)
	}
}

func TestValidateGitHubIgnoresGitLabConfig(t *testing.T) {
	cfg := validConfig()
	cfg.GitLabClientID = ""
	cfg.GitLabClientSecret = ""
	cfg.GitLabGroups = nil
	cfg.GitLabUsers = nil
	if err := cfg.Validate(); err != nil {
		t.Fatalf("GitLab settings must be ignored in GitHub mode: %v", err)
	}
}

func TestValidateUnsupportedProvider(t *testing.T) {
	cfg := validConfig()
	cfg.AuthProvider = "google"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for an unsupported provider")
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

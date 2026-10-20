package config

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/peterbourgon/ff/v3"

	"github.com/Thiht/pici/internal/ci"
)

const oidcScope = "openid"

type Config struct {
	HTTPAddr        string
	DBDriver        string
	DBDSN           string
	WorkspaceDir    string
	RepoMountPath   string
	Concurrency     int
	StepTimeout     time.Duration
	MaxSnapshotSize int64
	ConfigFile      string
	LogFormat       string

	SecretKey          string
	PublicBaseURL      string
	AuthProvider       string
	GitHubClientID     string
	GitHubClientSecret string
	GitHubTeams        []string
	GitHubUsers        []string
	GitLabURL          string
	GitLabClientID     string
	GitLabClientSecret string
	GitLabGroups       []string
	GitLabUsers        []string
	OIDCIssuer         string
	OIDCClientID       string
	OIDCClientSecret   string
	OIDCGroups         []string
	OIDCUsers          []string
	OIDCScopes         []string
	OIDCDisplayName    string
	SessionTTL         time.Duration
	GCInterval         time.Duration
	GCKeepDuration     time.Duration
	SchedulerInterval  time.Duration
	ShutdownTimeout    time.Duration
}

func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("pici", flag.ContinueOnError)
	var cfg Config
	var githubTeams, githubUsers, gitlabGroups, gitlabUsers string
	var oidcGroups, oidcUsers, oidcScopes string

	fs.StringVar(&cfg.HTTPAddr, "http-addr", ":8080", "HTTP listen address")
	fs.StringVar(&cfg.DBDriver, "db-driver", "sqlite", "database driver (sqlite, postgres)")
	fs.StringVar(&cfg.DBDSN, "db-dsn", "pici.db", "database DSN (path for sqlite, connection string for postgres)")
	fs.StringVar(&cfg.WorkspaceDir, "workspace-dir", defaultWorkspaceDir(), "directory for clones and logs")
	fs.StringVar(&cfg.RepoMountPath, "repo-mount-path", "/workspace", "mount path of the repo inside the runner container")
	fs.IntVar(&cfg.Concurrency, "concurrency", 4, "maximum concurrent executions")
	fs.DurationVar(&cfg.StepTimeout, "step-timeout", 30*time.Minute, "default step timeout")
	fs.Int64Var(&cfg.MaxSnapshotSize, "max-snapshot-size", 512<<20, "maximum size in bytes of an uploaded local snapshot")
	fs.StringVar(&cfg.ConfigFile, "config", "", "path to a JSON config file")
	fs.StringVar(&cfg.LogFormat, "log-format", "text", "log format (text, json)")

	fs.StringVar(&cfg.SecretKey, "secret-key", "", "32-byte key (hex or base64) used to encrypt secrets at rest (required)")
	fs.StringVar(&cfg.PublicBaseURL, "public-url", "", "public base URL, e.g. https://pici.example.com (required)")
	fs.StringVar(&cfg.AuthProvider, "auth-provider", "github", "identity provider for sign-in (github, gitlab, oidc)")
	fs.StringVar(&cfg.GitHubClientID, "github-client-id", "", "GitHub OAuth App client id (required with -auth-provider=github)")
	fs.StringVar(&cfg.GitHubClientSecret, "github-client-secret", "", "GitHub OAuth App client secret (required with -auth-provider=github)")
	fs.StringVar(&githubTeams, "github-teams", "", "comma-separated GitHub teams allowed to sign in, e.g. acme/pici-users")
	fs.StringVar(&githubUsers, "github-users", "", "comma-separated GitHub logins allowed to sign in, e.g. octocat")
	fs.StringVar(&cfg.GitLabURL, "gitlab-url", "https://gitlab.com", "base URL of the GitLab instance")
	fs.StringVar(&cfg.GitLabClientID, "gitlab-client-id", "", "GitLab OAuth application id (required with -auth-provider=gitlab)")
	fs.StringVar(&cfg.GitLabClientSecret, "gitlab-client-secret", "", "GitLab OAuth application secret (required with -auth-provider=gitlab)")
	fs.StringVar(&gitlabGroups, "gitlab-groups", "", "comma-separated GitLab groups allowed to sign in, e.g. acme/pici-users")
	fs.StringVar(&gitlabUsers, "gitlab-users", "", "comma-separated GitLab usernames allowed to sign in, e.g. alice")
	fs.StringVar(&cfg.OIDCIssuer, "oidc-issuer", "", "OpenID Connect issuer URL, e.g. https://keycloak.example.com/realms/pici")
	fs.StringVar(&cfg.OIDCClientID, "oidc-client-id", "", "OpenID Connect client id (required with -auth-provider=oidc)")
	fs.StringVar(&cfg.OIDCClientSecret, "oidc-client-secret", "", "OpenID Connect client secret (required with -auth-provider=oidc)")
	fs.StringVar(&oidcGroups, "oidc-groups", "", "comma-separated groups allowed to sign in, from the groups claim")
	fs.StringVar(&oidcUsers, "oidc-users", "", "comma-separated usernames or emails allowed to sign in")
	fs.StringVar(&oidcScopes, "oidc-scopes", "", "space or comma separated OAuth scopes (default: openid profile email offline_access)")
	fs.StringVar(&cfg.OIDCDisplayName, "oidc-name", "SSO", "display name of the OIDC provider, e.g. Keycloak")
	fs.DurationVar(&cfg.SessionTTL, "session-ttl", 30*24*time.Hour, "browser session lifetime")
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

	cfg.PublicBaseURL = strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/")
	cfg.AuthProvider = strings.ToLower(strings.TrimSpace(cfg.AuthProvider))
	cfg.LogFormat = strings.ToLower(strings.TrimSpace(cfg.LogFormat))
	cfg.GitLabURL = strings.TrimRight(strings.TrimSpace(cfg.GitLabURL), "/")
	cfg.GitHubTeams = splitList(githubTeams)
	cfg.GitHubUsers = splitList(githubUsers)
	cfg.GitLabGroups = splitList(gitlabGroups)
	cfg.GitLabUsers = splitList(gitlabUsers)
	cfg.OIDCGroups = splitList(oidcGroups)
	cfg.OIDCUsers = splitList(oidcUsers)
	cfg.OIDCScopes = splitScopes(oidcScopes)
	if len(cfg.OIDCScopes) == 0 {
		cfg.OIDCScopes = []string{"openid", "profile", "email", "offline_access"}
	}
	cfg.OIDCDisplayName = strings.TrimSpace(cfg.OIDCDisplayName)
	if cfg.OIDCDisplayName == "" {
		cfg.OIDCDisplayName = "SSO"
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	switch c.LogFormat {
	case "", "text", "json":
	default:
		return fmt.Errorf("invalid log format %q (want text or json)", c.LogFormat)
	}
	if c.SecretKey == "" {
		return errors.New("PICI_SECRET_KEY is required (generate one with `openssl rand -hex 32`)")
	}
	if c.PublicBaseURL == "" {
		return errors.New("PICI_PUBLIC_URL is required, e.g. https://pici.example.com")
	}
	u, err := url.Parse(c.PublicBaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("PICI_PUBLIC_URL is not a valid URL: %q", c.PublicBaseURL)
	}
	if !strings.EqualFold(u.Scheme, "https") && !isLocalHost(u.Hostname()) {
		return fmt.Errorf("PICI_PUBLIC_URL must use https: %q", c.PublicBaseURL)
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("PICI_PUBLIC_URL must be an origin without credentials, path, query or fragment: %q", c.PublicBaseURL)
	}
	switch c.AuthProvider {
	case "", "github":
		if c.GitHubClientID == "" {
			return errors.New("PICI_GITHUB_CLIENT_ID is required")
		}
		if c.GitHubClientSecret == "" {
			return errors.New("PICI_GITHUB_CLIENT_SECRET is required")
		}
		if len(c.GitHubTeams) == 0 && len(c.GitHubUsers) == 0 {
			return errors.New("PICI_GITHUB_TEAMS or PICI_GITHUB_USERS is required, e.g. acme/pici-users or octocat")
		}
		for _, team := range c.GitHubTeams {
			parts := strings.Split(team, "/")
			if len(parts) != 2 ||
				parts[0] == "" || parts[1] == "" ||
				strings.TrimSpace(parts[0]) != parts[0] || strings.TrimSpace(parts[1]) != parts[1] {
				return fmt.Errorf("invalid GitHub team %q (want org/team)", team)
			}
		}
		for _, user := range c.GitHubUsers {
			if user == "" || strings.ContainsAny(user, "/ \t") {
				return fmt.Errorf("invalid GitHub user %q", user)
			}
		}
	case "gitlab":
		if c.GitLabClientID == "" {
			return errors.New("PICI_GITLAB_CLIENT_ID is required")
		}
		if c.GitLabClientSecret == "" {
			return errors.New("PICI_GITLAB_CLIENT_SECRET is required")
		}
		u, err := url.Parse(c.GitLabURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return fmt.Errorf("PICI_GITLAB_URL is not a valid URL: %q", c.GitLabURL)
		}
		if len(c.GitLabGroups) == 0 && len(c.GitLabUsers) == 0 {
			return errors.New("PICI_GITLAB_GROUPS or PICI_GITLAB_USERS is required, e.g. acme/pici-users or alice")
		}
		for _, group := range c.GitLabGroups {
			if strings.HasPrefix(group, "/") || strings.HasSuffix(group, "/") || strings.Contains(group, "//") {
				return fmt.Errorf("invalid GitLab group %q (want a full path like acme/pici-users)", group)
			}
			for segment := range strings.SplitSeq(group, "/") {
				if segment == "" || strings.TrimSpace(segment) != segment || strings.ContainsAny(segment, " \t") {
					return fmt.Errorf("invalid GitLab group %q (want a full path like acme/pici-users)", group)
				}
			}
		}
		for _, user := range c.GitLabUsers {
			if user == "" || strings.ContainsAny(user, "/ \t") {
				return fmt.Errorf("invalid GitLab user %q", user)
			}
		}
	case "oidc":
		if c.OIDCIssuer == "" {
			return errors.New("PICI_OIDC_ISSUER is required")
		}
		u, err := url.Parse(c.OIDCIssuer)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return fmt.Errorf("PICI_OIDC_ISSUER is not a valid URL: %q", c.OIDCIssuer)
		}
		if c.OIDCClientID == "" {
			return errors.New("PICI_OIDC_CLIENT_ID is required")
		}
		if c.OIDCClientSecret == "" {
			return errors.New("PICI_OIDC_CLIENT_SECRET is required")
		}
		if len(c.OIDCGroups) == 0 && len(c.OIDCUsers) == 0 {
			return errors.New("PICI_OIDC_GROUPS or PICI_OIDC_USERS is required, e.g. acme/pici-users or alice")
		}
		for _, group := range c.OIDCGroups {
			if group == "" {
				return fmt.Errorf("invalid OIDC group %q", group)
			}
		}
		for _, user := range c.OIDCUsers {
			if user == "" || strings.ContainsAny(user, "/ \t") {
				return fmt.Errorf("invalid OIDC user %q", user)
			}
		}
		if !slices.Contains(c.OIDCScopes, oidcScope) {
			return fmt.Errorf("PICI_OIDC_SCOPES must include %q", oidcScope)
		}
	default:
		return fmt.Errorf("unsupported auth provider %q (want github, gitlab or oidc)", c.AuthProvider)
	}
	if c.SessionTTL <= 0 {
		return errors.New("PICI_SESSION_TTL must be greater than zero")
	}
	if c.MaxSnapshotSize <= 0 {
		return errors.New("PICI_MAX_SNAPSHOT_SIZE must be greater than zero")
	}
	repo := strings.TrimRight(filepath.Clean(c.RepoMountPath), string(filepath.Separator)) + string(filepath.Separator)
	cache := strings.TrimRight(filepath.Clean(ci.CacheMountPath), string(filepath.Separator)) + string(filepath.Separator)
	if strings.HasPrefix(repo, cache) || strings.HasPrefix(cache, repo) {
		return fmt.Errorf("PICI_REPO_MOUNT_PATH (%s) must not overlap the cache mount path (%s)", c.RepoMountPath, ci.CacheMountPath)
	}
	return nil
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func splitScopes(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
}

func isLocalHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost"
}

func defaultWorkspaceDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home + "/.pici/workspaces"
}

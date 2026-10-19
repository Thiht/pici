package stores

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
	"uuid"
)

// User is a human identity authenticated through an external provider.
// The OAuth tokens are stored encrypted at rest and only used for membership
// checks. Providers that issue non-expiring tokens leave OAuthRefreshToken
// empty and OAuthExpiresAt nil.
type User struct {
	ID                uuid.UUID  `json:"id"`
	Provider          string     `json:"provider"`
	ProviderUserID    string     `json:"-"`
	Login             string     `json:"login"`
	Name              string     `json:"name"`
	AvatarURL         string     `json:"avatar_url"`
	OAuthAccessToken  string     `json:"-"`
	OAuthRefreshToken string     `json:"-"`
	OAuthExpiresAt    *time.Time `json:"-"`
	CreatedAt         time.Time  `json:"created_at"`
	LastLoginAt       time.Time  `json:"last_login_at"`
	LastOrgCheckAt    *time.Time `json:"last_org_check_at,omitempty"`
	DisabledAt        *time.Time `json:"disabled_at,omitempty"`
}

// WithoutOAuthTokens returns a copy without the provider credentials, which
// handlers never need.
func (u User) WithoutOAuthTokens() User {
	u.OAuthAccessToken = ""
	u.OAuthRefreshToken = ""
	u.OAuthExpiresAt = nil
	return u
}

// Session is a browser session. ID is the SHA-256 hash of the session cookie
// value; the raw token is never stored.
type Session struct {
	ID         string    `json:"-"`
	UserID     uuid.UUID `json:"-"`
	CSRFToken  string    `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	UserAgent  string    `json:"user_agent"`
}

// APIToken is a personal token used by the CLI and scripts. TokenHash is the
// SHA-256 hash of the raw token; the raw value is only shown when created.
type APIToken struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"-"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	TokenHash  string     `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

type Project struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	RepoURL       string    `json:"repo_url"`
	Provider      Provider  `json:"provider"`
	AuthType      AuthType  `json:"auth_type"`
	AuthUser      string    `json:"auth_user"`
	AuthSecret    string    `json:"-"`
	WebhookSecret string    `json:"-"`
	DefaultBranch string    `json:"default_branch"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Variable struct {
	ProjectID *uuid.UUID `json:"project_id"`
	Key       string     `json:"key"`
	Value     string     `json:"value"`
	Secret    bool       `json:"secret"`
}

type StepResult struct {
	Name       string     `json:"name"`
	Status     StepStatus `json:"status"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	ExitCode   int        `json:"exit_code"`
	Error      string     `json:"error,omitempty"`
	Env        []string   `json:"env,omitempty"`
}

// Steps is a JSON-serialized list of step results, stored in a single column.
type Steps []StepResult

func (s Steps) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (s *Steps) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case nil:
		b = nil
	case string:
		b = []byte(v)
	case []byte:
		b = v
	default:
		return fmt.Errorf("invalid type for Steps: %T", src)
	}
	if len(b) == 0 {
		*s = nil
		return nil
	}
	return json.Unmarshal(b, s)
}

type Execution struct {
	ID              int64      `json:"id"`
	ProjectID       uuid.UUID  `json:"project_id"`
	Project         string     `json:"project,omitempty"`
	Workflow        string     `json:"workflow"`
	Ref             string     `json:"ref"`
	CommitSHA       string     `json:"commit_sha"`
	Status          Status     `json:"status"`
	Trigger         Trigger    `json:"trigger"`
	Source          Source     `json:"source"`
	Steps           Steps      `json:"steps,omitempty"`
	Error           string     `json:"error,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	SetupFinishedAt *time.Time `json:"setup_finished_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`

	ClaimedBy        string     `json:"-"`
	CancelRequested  bool       `json:"-"`
	ConcurrencyGroup string     `json:"-"`
	ParentID         *int64     `json:"parent_id,omitempty"`
	WorkspaceID      *int64     `json:"-"`
	SnapshotID       *uuid.UUID `json:"-"`
	CreatedBy        *uuid.UUID `json:"created_by,omitempty"`
	CreatedByLogin   string     `json:"created_by_login,omitempty"`
}

type Schedule struct {
	ProjectID uuid.UUID
	Workflow  string
	CronExpr  string
	NextRunAt time.Time
}

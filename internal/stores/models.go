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
	Name       string        `json:"name"`
	Status     StepStatus    `json:"status"`
	StartedAt  *time.Time    `json:"started_at,omitempty"`
	FinishedAt *time.Time    `json:"finished_at,omitempty"`
	Timeout    time.Duration `json:"timeout,omitempty"`
	ExitCode   int           `json:"exit_code"`
	Error      string        `json:"error,omitempty"`
	Env        []string      `json:"env,omitempty"`
}

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

// Execution is the Execution view of a selected attempt, shared by the API and
// the web UI. ExecutionID, Attempt and AttemptsCount identify the logical
// Execution; ID, status, steps and timestamps belong to that selected attempt.
// The JSON form exposes the Execution id as "id".
type Execution struct {
	ID            int64     `json:"-"`
	ExecutionID   int64     `json:"id"`
	Attempt       int       `json:"attempt"`
	AttemptsCount int       `json:"attempts_count"`
	ProjectID     uuid.UUID `json:"project_id"`
	Project       string    `json:"project,omitempty"`
	Workflow      string    `json:"workflow"`
	Ref           string    `json:"ref"`
	CommitSHA     string    `json:"commit_sha"`
	Status        Status    `json:"status"`
	Trigger       Trigger   `json:"trigger"`
	Source        Source    `json:"source"`
	Steps         Steps     `json:"steps,omitempty"`
	Error         string    `json:"error,omitempty"`
	// CreatedAt is when the Execution was created; AttemptCreatedAt is when
	// this attempt was created. Both are the same on the first attempt.
	CreatedAt        time.Time  `json:"created_at"`
	AttemptCreatedAt time.Time  `json:"-"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	SetupFinishedAt  *time.Time `json:"setup_finished_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`

	ClaimedBy        string `json:"-"`
	CancelRequested  bool   `json:"-"`
	ConcurrencyGroup string `json:"-"`
	// ParentID is the Execution this one was rebuilt from. It is empty for
	// retry attempts, whose previous attempt is internal to the Execution.
	ParentID *int64 `json:"parent_id,omitempty"`
	// PreviousAttemptID is the attempt this row retries. It is only used inside
	// an Execution to carry over successful steps and logs.
	PreviousAttemptID *int64     `json:"-"`
	WorkspaceID       *int64     `json:"-"`
	SnapshotID        *uuid.UUID `json:"-"`
	CheckRunID        int64      `json:"-"`
	CreatedBy         *uuid.UUID `json:"created_by,omitempty"`
	CreatedByLogin    string     `json:"created_by_login,omitempty"`
}

type Attempt struct {
	Number         int        `json:"attempt"`
	Status         Status     `json:"status"`
	Trigger        Trigger    `json:"trigger"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	CreatedBy      *uuid.UUID `json:"created_by,omitempty"`
	CreatedByLogin string     `json:"created_by_login,omitempty"`
}

func (e Execution) AttemptSummary() Attempt {
	return Attempt{
		Number:         e.Attempt,
		Status:         e.Status,
		Trigger:        e.Trigger,
		Error:          e.Error,
		CreatedAt:      e.CreatedAt,
		StartedAt:      e.StartedAt,
		FinishedAt:     e.FinishedAt,
		CreatedBy:      e.CreatedBy,
		CreatedByLogin: e.CreatedByLogin,
	}
}

type Schedule struct {
	ProjectID uuid.UUID
	Workflow  string
	CronExpr  string
	NextRunAt time.Time
}

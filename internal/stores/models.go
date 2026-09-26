package stores

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
	"uuid"
)

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
	if len(b) == 0 || string(b) == "[]" {
		*s = nil
		return nil
	}
	return json.Unmarshal(b, s)
}

type Execution struct {
	ID         uuid.UUID  `json:"id"`
	ProjectID  uuid.UUID  `json:"project_id"`
	Project    string     `json:"project,omitempty"`
	Workflow   string     `json:"workflow"`
	Ref        string     `json:"ref"`
	CommitSHA  string     `json:"commit_sha"`
	Status     Status     `json:"status"`
	Trigger    Trigger    `json:"trigger"`
	Steps      Steps      `json:"steps,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`

	ClaimedBy        string `json:"-"`
	CancelRequested  bool   `json:"-"`
	ConcurrencyGroup string `json:"-"`
}

type Schedule struct {
	ProjectID uuid.UUID
	Workflow  string
	CronExpr  string
	NextRunAt time.Time
}

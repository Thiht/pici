package stores

type Project struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RepoURL       string `json:"repo_url"`
	Provider      string `json:"provider"`
	AuthType      string `json:"auth_type"`
	AuthUser      string `json:"auth_user"`
	AuthSecret    string `json:"-"`
	WebhookSecret string `json:"-"`
	DefaultBranch string `json:"default_branch"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

const (
	ProviderGitHub  = "github"
	ProviderGitLab  = "gitlab"
	ProviderGeneric = "generic"
	AuthTypeNone    = "none"
	AuthTypeToken   = "token"
	AuthTypeSSH     = "ssh"
)

type Variable struct {
	ProjectID string `json:"project_id"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Secret    bool   `json:"secret"`
}

type StepResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
	ExitCode   int    `json:"exit_code"`
	Error      string `json:"error,omitempty"`
}

type Execution struct {
	ID         string       `json:"id"`
	ProjectID  string       `json:"project_id"`
	Project    string       `json:"project,omitempty"`
	Workflow   string       `json:"workflow"`
	Ref        string       `json:"ref"`
	CommitSHA  string       `json:"commit_sha"`
	Status     string       `json:"status"`
	Trigger    string       `json:"trigger"`
	Steps      []StepResult `json:"steps,omitempty"`
	Error      string       `json:"error,omitempty"`
	StartedAt  int64        `json:"started_at"`
	FinishedAt int64        `json:"finished_at"`
	CreatedAt  int64        `json:"created_at"`

	ClaimedBy        string `json:"-"`
	CancelRequested  bool   `json:"-"`
	ConcurrencyGroup string `json:"-"`
}

type Schedule struct {
	ProjectID string
	Workflow  string
	CronExpr  string
	NextRunAt int64
}

const (
	StatusPending  = "pending"
	StatusRunning  = "running"
	StatusSuccess  = "success"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"

	StepStatusPending  = "pending"
	StepStatusRunning  = "running"
	StepStatusSuccess  = "success"
	StepStatusFailed   = "failed"
	StepStatusSkipped  = "skipped"
	StepStatusCanceled = "canceled"

	TriggerManual  = "manual"
	TriggerWebhook = "webhook"
	TriggerCron    = "cron"
	TriggerRebuild = "rebuild"
)

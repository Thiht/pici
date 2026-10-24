package stores

//go:generate go tool go-enum --marshal --sql --names

// ENUM(github, gitlab, generic)
type Provider string

// ENUM(none, token, ssh)
type AuthType string

// ENUM(pending, running, success, failed, canceled)
type Status string

// ENUM(manual, webhook, cron, rebuild, retry)
type Trigger string

// ENUM(pending, running, success, failed, skipped, canceled)
type StepStatus string

// ENUM(git, snapshot)
type Source string

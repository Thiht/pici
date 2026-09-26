package stores

//go:generate go tool go-enum --marshal --sql --names

// Provider identifies the git hosting provider of a project.
// ENUM(github, gitlab, generic)
type Provider string

// AuthType is the authentication method used to clone a repository.
// ENUM(none, token, ssh)
type AuthType string

// Status is the lifecycle status of an execution.
// ENUM(pending, running, success, failed, canceled)
type Status string

// Trigger is what caused an execution to run.
// ENUM(manual, webhook, cron, rebuild)
type Trigger string

// StepStatus is the lifecycle status of a single step.
// ENUM(pending, running, success, failed, skipped, canceled)
type StepStatus string

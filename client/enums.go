package client

import "github.com/Thiht/pici/internal/stores"

type (
	Provider   = stores.Provider
	AuthType   = stores.AuthType
	Status     = stores.Status
	Trigger    = stores.Trigger
	StepStatus = stores.StepStatus
)

const (
	ProviderGithub  = stores.ProviderGithub
	ProviderGitlab  = stores.ProviderGitlab
	ProviderGeneric = stores.ProviderGeneric

	AuthTypeNone  = stores.AuthTypeNone
	AuthTypeToken = stores.AuthTypeToken
	AuthTypeSsh   = stores.AuthTypeSsh

	StatusPending  = stores.StatusPending
	StatusRunning  = stores.StatusRunning
	StatusSuccess  = stores.StatusSuccess
	StatusFailed   = stores.StatusFailed
	StatusCanceled = stores.StatusCanceled

	TriggerManual  = stores.TriggerManual
	TriggerWebhook = stores.TriggerWebhook
	TriggerCron    = stores.TriggerCron
	TriggerRebuild = stores.TriggerRebuild

	StepStatusPending  = stores.StepStatusPending
	StepStatusRunning  = stores.StepStatusRunning
	StepStatusSuccess  = stores.StepStatusSuccess
	StepStatusFailed   = stores.StepStatusFailed
	StepStatusSkipped  = stores.StepStatusSkipped
	StepStatusCanceled = stores.StepStatusCanceled
)

var (
	ParseProvider   = stores.ParseProvider
	ParseAuthType   = stores.ParseAuthType
	ParseStatus     = stores.ParseStatus
	ParseTrigger    = stores.ParseTrigger
	ParseStepStatus = stores.ParseStepStatus
)

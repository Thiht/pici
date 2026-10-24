// Package authtest provides a configurable auth.Provider for tests.
package authtest

import (
	"context"
	"net/url"

	"github.com/Thiht/pici/internal/auth"
)

// Provider is an auth.Provider whose every answer is a field. The zero value
// behaves like a GitHub provider that only knows one team-less user; calls the
// service makes are counted so a test can assert on them.
type Provider struct {
	ProviderName    string
	ProviderDisplay string
	ProviderNoun    string
	// Scope overrides RequiredScope. When empty, a GitHub provider requires
	// read:org and the others require nothing, as the real ones do.
	Scope string

	Identity  auth.Identity
	TeamList  []auth.Team
	Refreshed auth.Token

	ExchangeErr error
	RefreshErr  error
	TeamsErr    error
	VerifyErr   error

	RefreshCalls int
	LastRefresh  string
	TeamsCalls   int
	VerifyCalls  int
	LastToken    string
	LastVerifier string
}

func (p *Provider) Name() string {
	if p.ProviderName != "" {
		return p.ProviderName
	}
	return "github"
}

func (p *Provider) DisplayName() string {
	if p.ProviderDisplay != "" {
		return p.ProviderDisplay
	}
	switch p.Name() {
	case "gitlab":
		return "GitLab"
	case "oidc":
		return "SSO"
	default:
		return "GitHub"
	}
}

func (p *Provider) AccessNoun() string {
	if p.ProviderNoun != "" {
		return p.ProviderNoun
	}
	if p.Name() == "github" {
		return "team"
	}
	return "group"
}

func (p *Provider) RequiredScope() string {
	if p.Scope != "" {
		return p.Scope
	}
	if p.Name() == "github" {
		return "read:org"
	}
	return ""
}

func (p *Provider) AuthCodeURL(state, nonce, verifier string) string {
	params := url.Values{"state": {state}}
	if nonce != "" {
		params.Set("nonce", nonce)
	}
	if verifier != "" {
		params.Set("code_challenge", verifier)
	}
	return "https://auth.test/authorize?" + params.Encode()
}

func (p *Provider) Exchange(_ context.Context, _, _, verifier string) (auth.Identity, error) {
	p.LastVerifier = verifier
	if p.ExchangeErr != nil {
		return auth.Identity{}, p.ExchangeErr
	}
	return p.Identity, nil
}

func (p *Provider) Refresh(_ context.Context, refreshToken string) (auth.Token, error) {
	p.RefreshCalls++
	p.LastRefresh = refreshToken
	if p.RefreshErr != nil {
		return auth.Token{}, p.RefreshErr
	}
	return p.Refreshed, nil
}

func (p *Provider) Teams(_ context.Context, accessToken string) ([]auth.Team, error) {
	p.TeamsCalls++
	p.LastToken = accessToken
	if p.TeamsErr != nil {
		return nil, p.TeamsErr
	}
	return p.TeamList, nil
}

func (p *Provider) Verify(_ context.Context, accessToken string) error {
	p.VerifyCalls++
	p.LastToken = accessToken
	return p.VerifyErr
}

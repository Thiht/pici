package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
)

const (
	githubAuthURL  = "https://github.com/login/oauth/authorize"
	githubTokenURL = "https://github.com/login/oauth/access_token"
	githubAPIURL   = "https://api.github.com"
)

// GitHubProvider implements Provider for github.com OAuth Apps.
type GitHubProvider struct {
	flow   *flow
	apiURL string
}

// NewGitHubProvider builds the github.com provider. withTeams controls whether
// the read:org scope is requested: personal instances that only allowlist
// logins do not need it.
func NewGitHubProvider(clientID, clientSecret, redirectURL string, withTeams bool) *GitHubProvider {
	return newGitHubProvider(clientID, clientSecret, redirectURL, githubAuthURL, githubTokenURL, githubAPIURL, withTeams)
}

// newGitHubProvider allows tests to point at a local server. The public
// constructor is deliberately github.com-only.
func newGitHubProvider(clientID, clientSecret, redirectURL, authURL, tokenURL, apiURL string, withTeams bool) *GitHubProvider {
	var scopes []string
	if withTeams {
		scopes = []string{"read:org"}
	}
	return &GitHubProvider{
		flow: newFlow(
			&oauth2.Config{
				ClientID:     clientID,
				ClientSecret: clientSecret,
				RedirectURL:  redirectURL,
				Scopes:       scopes,
				Endpoint: oauth2.Endpoint{
					AuthURL:  authURL,
					TokenURL: tokenURL,
				},
			},
			map[string]string{
				"Accept":               "application/vnd.github+json",
				"X-GitHub-Api-Version": "2022-11-28",
			},
			httpPolicy{
				NotFoundInvalid: true,
				Forbidden: func(resp *http.Response) error {
					// GitHub uses 403 for rate limits *and* for org restrictions
					// or SSO enforcement. Only the former is transient.
					if resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "" {
						return fmt.Errorf("%w: rate limited", ErrProviderUnavailable)
					}
					return ErrTokenInvalid
				},
			},
		),
		apiURL: strings.TrimRight(apiURL, "/"),
	}
}

func (p *GitHubProvider) Name() string        { return "github" }
func (p *GitHubProvider) DisplayName() string { return "GitHub" }
func (p *GitHubProvider) AccessNoun() string  { return "team" }

// RequiredScope is read:org, and only in team mode.
func (p *GitHubProvider) RequiredScope() string {
	if len(p.flow.config.Scopes) == 0 {
		return ""
	}
	return "read:org"
}

func (p *GitHubProvider) AuthCodeURL(state, _, verifier string) string {
	return p.flow.AuthCodeURL(state, "", verifier)
}

func (p *GitHubProvider) Exchange(ctx context.Context, code, _, verifier string) (Identity, error) {
	token, err := p.flow.Exchange(ctx, code, verifier)
	if err != nil {
		return Identity{}, err
	}

	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := p.flow.getJSON(ctx, token.AccessToken, p.apiURL+"/user", &user); err != nil {
		return Identity{}, err
	}
	if user.ID == 0 || user.Login == "" {
		return Identity{}, fmt.Errorf("%w: incomplete GitHub user profile", ErrProviderUnavailable)
	}

	scope, _ := token.Extra("scope").(string)
	return Identity{
		ProviderUserID: strconv.FormatInt(user.ID, 10),
		Login:          user.Login,
		Name:           user.Name,
		AvatarURL:      user.AvatarURL,
		Token:          tokenFromOAuth(token),
		Scopes:         parseScopes(scope),
	}, nil
}

func (p *GitHubProvider) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	return p.flow.Refresh(ctx, refreshToken)
}

func (p *GitHubProvider) Teams(ctx context.Context, accessToken string) ([]Team, error) {
	var teams []Team
	next := p.apiURL + "/user/teams?per_page=100"
	for next != "" {
		resp, err := p.flow.get(ctx, accessToken, next)
		if err != nil {
			return nil, err
		}
		var page []struct {
			Slug         string `json:"slug"`
			Organization struct {
				Login string `json:"login"`
			} `json:"organization"`
		}
		err = json.NewDecoder(resp.Body).Decode(&page)
		next = nextLink(resp.Header.Get("Link"))
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode GitHub teams: %w", err)
		}
		for _, team := range page {
			teams = append(teams, Team{Path: team.Organization.Login + "/" + team.Slug})
		}
	}
	return teams, nil
}

func (p *GitHubProvider) Verify(ctx context.Context, accessToken string) error {
	var user struct{}
	return p.flow.getJSON(ctx, accessToken, p.apiURL+"/user", &user)
}

func nextLink(header string) string {
	for part := range strings.SplitSeq(header, ",") {
		sections := strings.Split(part, ";")
		if len(sections) < 2 {
			continue
		}
		if strings.TrimSpace(sections[1]) != `rel="next"` {
			continue
		}
		return strings.Trim(strings.TrimSpace(sections[0]), "<>")
	}
	return ""
}

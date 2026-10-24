package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
)

// GitLabProvider implements Provider for gitlab.com and self-hosted GitLab
// instances.
type GitLabProvider struct {
	flow   *flow
	apiURL string
}

// NewGitLabProvider builds a provider for the given GitLab base URL.
func NewGitLabProvider(baseURL, clientID, clientSecret, redirectURL string) *GitLabProvider {
	base := strings.TrimRight(baseURL, "/")
	return &GitLabProvider{
		flow: newFlow(
			&oauth2.Config{
				ClientID:     clientID,
				ClientSecret: clientSecret,
				RedirectURL:  redirectURL,
				// read_api is enough to read the user profile and list groups.
				// offline_access makes GitLab issue a refresh token: access
				// tokens expire after two hours by default.
				Scopes: []string{"read_api", "offline_access"},
				Endpoint: oauth2.Endpoint{
					AuthURL:  base + "/oauth/authorize",
					TokenURL: base + "/oauth/token",
				},
			},
			nil,
			httpPolicy{
				NotFoundInvalid: true,
				Forbidden: func(*http.Response) error {
					return ErrAccessDenied
				},
			},
		),
		apiURL: base + "/api/v4",
	}
}

func (p *GitLabProvider) Name() string        { return "gitlab" }
func (p *GitLabProvider) DisplayName() string { return "GitLab" }
func (p *GitLabProvider) AccessNoun() string  { return "group" }

// RequiredScope is empty: the read_api scope grants both profile and group
// access, so a missing scope is reported as a provider error rather than as a
// membership denial.
func (p *GitLabProvider) RequiredScope() string { return "" }

func (p *GitLabProvider) AuthCodeURL(state, _, verifier string) string {
	return p.flow.AuthCodeURL(state, "", verifier)
}

func (p *GitLabProvider) Exchange(ctx context.Context, code, _, verifier string) (Identity, error) {
	token, err := p.flow.Exchange(ctx, code, verifier)
	if err != nil {
		return Identity{}, err
	}

	var user struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := p.flow.getJSON(ctx, token.AccessToken, p.apiURL+"/user", &user); err != nil {
		return Identity{}, err
	}
	if user.ID == 0 || user.Username == "" {
		return Identity{}, fmt.Errorf("%w: incomplete GitLab user profile", ErrProviderUnavailable)
	}

	scope, _ := token.Extra("scope").(string)
	return Identity{
		ProviderUserID: strconv.FormatInt(user.ID, 10),
		Login:          user.Username,
		Name:           user.Name,
		AvatarURL:      user.AvatarURL,
		Token:          tokenFromOAuth(token),
		Scopes:         parseScopes(scope),
	}, nil
}

func (p *GitLabProvider) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	return p.flow.Refresh(ctx, refreshToken)
}

// Teams lists the groups the user is a member of, including inherited
// membership of parent groups. GitLab returns a group's full path, so
// subgroups are matched by their complete path.
func (p *GitLabProvider) Teams(ctx context.Context, accessToken string) ([]Team, error) {
	var teams []Team
	page := "1"
	for page != "" {
		resp, err := p.flow.get(ctx, accessToken, p.apiURL+"/groups?min_access_level=10&per_page=100&page="+url.QueryEscape(page))
		if err != nil {
			return nil, err
		}
		var groups []struct {
			FullPath string `json:"full_path"`
		}
		err = json.NewDecoder(resp.Body).Decode(&groups)
		page = resp.Header.Get("X-Next-Page")
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode GitLab groups: %w", err)
		}
		for _, group := range groups {
			teams = append(teams, Team{Path: group.FullPath})
		}
	}
	return teams, nil
}

func (p *GitLabProvider) Verify(ctx context.Context, accessToken string) error {
	var user struct{}
	return p.flow.getJSON(ctx, accessToken, p.apiURL+"/user", &user)
}

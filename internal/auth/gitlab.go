package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// GitLabProvider implements Provider for gitlab.com and self-hosted GitLab
// instances.
type GitLabProvider struct {
	config *oauth2.Config
	apiURL string
	client *http.Client
}

// NewGitLabProvider builds a provider for the given GitLab base URL.
func NewGitLabProvider(baseURL, clientID, clientSecret, redirectURL string) *GitLabProvider {
	base := strings.TrimRight(baseURL, "/")
	return &GitLabProvider{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			// read_api is enough to read the user profile and list groups.
			// offline_access makes GitLab issue a refresh token: access tokens
			// expire after two hours by default.
			Scopes: []string{"read_api", "offline_access"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  base + "/oauth/authorize",
				TokenURL: base + "/oauth/token",
			},
		},
		apiURL: base + "/api/v4",
		client: &http.Client{Timeout: 10 * time.Second},
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
	var opts []oauth2.AuthCodeOption
	if verifier != "" {
		opts = append(opts, oauth2.S256ChallengeOption(verifier))
	}
	return p.config.AuthCodeURL(state, opts...)
}

func (p *GitLabProvider) Exchange(ctx context.Context, code, _, verifier string) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var opts []oauth2.AuthCodeOption
	if verifier != "" {
		opts = append(opts, oauth2.VerifierOption(verifier))
	}
	token, err := p.config.Exchange(ctx, code, opts...)
	if err != nil {
		return Identity{}, err
	}

	var user struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := p.getJSON(ctx, token.AccessToken, "/user", &user); err != nil {
		return Identity{}, err
	}
	if user.ID == 0 || user.Username == "" {
		return Identity{}, fmt.Errorf("%w: incomplete GitLab user profile", ErrProviderUnavailable)
	}

	scope, _ := token.Extra("scope").(string)
	var expiresAt *time.Time
	if !token.Expiry.IsZero() {
		expiry := token.Expiry
		expiresAt = &expiry
	}
	return Identity{
		ProviderUserID: strconv.FormatInt(user.ID, 10),
		Login:          user.Username,
		Name:           user.Name,
		AvatarURL:      user.AvatarURL,
		Token: Token{
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			ExpiresAt:    expiresAt,
		},
		Scopes: parseScopes(scope),
	}, nil
}

// Refresh renews an expiring user token. GitLab rotates refresh tokens: the
// returned pair replaces the previous one, and the refresh token it replaces
// is invalidated as soon as this call succeeds.
func (p *GitLabProvider) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	token, err := p.config.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken}).Token()
	if err != nil {
		return Token{}, refreshError(err)
	}
	var expiresAt *time.Time
	if !token.Expiry.IsZero() {
		expiry := token.Expiry
		expiresAt = &expiry
	}
	return Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// Teams lists the groups the user is a member of, including inherited
// membership of parent groups. GitLab returns a group's full path, so
// subgroups are matched by their complete path.
func (p *GitLabProvider) Teams(ctx context.Context, accessToken string) ([]Team, error) {
	var teams []Team
	page := "1"
	for page != "" {
		resp, err := p.get(ctx, accessToken, p.apiURL+"/groups?min_access_level=10&per_page=100&page="+url.QueryEscape(page))
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
	return p.getJSON(ctx, accessToken, "/user", &user)
}

func (p *GitLabProvider) getJSON(ctx context.Context, accessToken, path string, dst any) error {
	resp, err := p.get(ctx, accessToken, p.apiURL+path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode GitLab %s: %w", path, err)
	}
	return nil
}

func (p *GitLabProvider) get(ctx context.Context, accessToken, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return resp, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, ErrTokenInvalid
	case resp.StatusCode == http.StatusForbidden:
		_ = resp.Body.Close()
		return nil, ErrAccessDenied
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: status %d", ErrProviderUnavailable, resp.StatusCode)
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: status %d", ErrProviderUnavailable, resp.StatusCode)
	}
}

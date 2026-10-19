package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const (
	githubAuthURL  = "https://github.com/login/oauth/authorize"
	githubTokenURL = "https://github.com/login/oauth/access_token"
	githubAPIURL   = "https://api.github.com"
)

// GitHubProvider implements Provider for github.com OAuth Apps.
type GitHubProvider struct {
	config *oauth2.Config
	apiURL string
	client *http.Client
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
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  authURL,
				TokenURL: tokenURL,
			},
		},
		apiURL: strings.TrimRight(apiURL, "/"),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *GitHubProvider) Name() string { return "github" }

func (p *GitHubProvider) AuthCodeURL(state, verifier string) string {
	var opts []oauth2.AuthCodeOption
	if verifier != "" {
		opts = append(opts, oauth2.S256ChallengeOption(verifier))
	}
	return p.config.AuthCodeURL(state, opts...)
}

func (p *GitHubProvider) Exchange(ctx context.Context, code, verifier string) (Identity, error) {
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
	if token.RefreshToken != "" || !token.Expiry.IsZero() {
		return Identity{}, fmt.Errorf("%w: disable expiring user tokens in the GitHub OAuth App settings", ErrExpiringTokens)
	}

	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := p.getJSON(ctx, token.AccessToken, "/user", &user); err != nil {
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
		AccessToken:    token.AccessToken,
		Scopes:         parseScopes(scope),
	}, nil
}

func (p *GitHubProvider) Teams(ctx context.Context, accessToken string) ([]Team, error) {
	var teams []Team
	next := p.apiURL + "/user/teams?per_page=100"
	for next != "" {
		resp, err := p.get(ctx, accessToken, next)
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
			teams = append(teams, Team{Org: team.Organization.Login, Slug: team.Slug})
		}
	}
	return teams, nil
}

func (p *GitHubProvider) Verify(ctx context.Context, accessToken string) error {
	var user struct{}
	return p.getJSON(ctx, accessToken, "/user", &user)
}

func (p *GitHubProvider) getJSON(ctx context.Context, accessToken, path string, dst any) error {
	resp, err := p.get(ctx, accessToken, p.apiURL+path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode GitHub %s: %w", path, err)
	}
	return nil
}

func (p *GitHubProvider) get(ctx context.Context, accessToken, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

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
		// GitHub uses 403 for rate limits *and* for org restrictions / SSO
		// enforcement. Only the former is transient.
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "" {
			return nil, fmt.Errorf("%w: rate limited", ErrProviderUnavailable)
		}
		return nil, ErrTokenInvalid
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: status %d", ErrProviderUnavailable, resp.StatusCode)
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: status %d", ErrProviderUnavailable, resp.StatusCode)
	}
}

func parseScopes(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' '
	})
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

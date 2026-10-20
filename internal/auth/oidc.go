package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const oidcDiscoveryTimeout = 15 * time.Second

// OIDCProvider implements Provider for any OpenID Connect 1.0 provider
// (Keycloak, Authentik, Entra ID, Google, Forgejo, ...).
type OIDCProvider struct {
	provider    *oidc.Provider
	verifier    *oidc.IDTokenVerifier
	config      *oauth2.Config
	userInfoURL string
	displayName string
	client      *http.Client
}

// NewOIDCProvider discovers the issuer configuration and returns a provider.
// Discovery happens at startup so a misconfigured issuer fails fast.
func NewOIDCProvider(ctx context.Context, issuerURL, clientID, clientSecret, redirectURL, displayName string, scopes []string) (*OIDCProvider, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcDiscoveryTimeout)
	defer cancel()

	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC issuer: %w", err)
	}
	if displayName == "" {
		displayName = "SSO"
	}
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email", "offline_access"}
	}
	return &OIDCProvider{
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       scopes,
			Endpoint:     provider.Endpoint(),
		},
		userInfoURL: provider.UserInfoEndpoint(),
		displayName: displayName,
		client:      &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (p *OIDCProvider) Name() string        { return "oidc" }
func (p *OIDCProvider) DisplayName() string { return p.displayName }
func (p *OIDCProvider) AccessNoun() string  { return "group" }

// RequiredScope is empty: the openid scope is mandatory for the exchange
// itself, and groups come from the userinfo endpoint rather than a dedicated
// OAuth scope check.
func (p *OIDCProvider) RequiredScope() string { return "" }

func (p *OIDCProvider) AuthCodeURL(state, nonce, verifier string) string {
	opts := []oauth2.AuthCodeOption{
		// Google only returns a refresh token when access_type=offline. Other
		// providers ignore the unknown parameter, per RFC 6749.
		oauth2.SetAuthURLParam("access_type", "offline"),
	}
	if nonce != "" {
		opts = append(opts, oidc.Nonce(nonce))
	}
	if verifier != "" {
		opts = append(opts, oauth2.S256ChallengeOption(verifier))
	}
	return p.config.AuthCodeURL(state, opts...)
}

func (p *OIDCProvider) Exchange(ctx context.Context, code, nonce, verifier string) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcDiscoveryTimeout)
	defer cancel()

	var opts []oauth2.AuthCodeOption
	if verifier != "" {
		opts = append(opts, oauth2.VerifierOption(verifier))
	}
	token, err := p.config.Exchange(ctx, code, opts...)
	if err != nil {
		return Identity{}, err
	}

	rawIDToken, _ := token.Extra("id_token").(string)
	if rawIDToken == "" {
		return Identity{}, fmt.Errorf("%w: the token response has no id_token", ErrProviderUnavailable)
	}
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: verify id_token: %v", ErrTokenInvalid, err)
	}
	if nonce != "" && subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		return Identity{}, fmt.Errorf("%w: id_token nonce mismatch", ErrTokenInvalid)
	}

	var claims oidcClaims
	if err := idToken.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("decode id_token claims: %w", err)
	}
	info, err := p.userInfo(ctx, token.AccessToken)
	if err != nil {
		return Identity{}, err
	}
	if info.Subject == "" || info.Subject != idToken.Subject {
		return Identity{}, fmt.Errorf("%w: userinfo subject does not match id_token", ErrTokenInvalid)
	}
	claims.merge(info)

	login := claims.PreferredUsername
	if login == "" {
		login = claims.Email
	}
	if login == "" {
		login = idToken.Subject
	}
	name := claims.Name
	if name == "" {
		name = login
	}

	scope, _ := token.Extra("scope").(string)
	var expiresAt *time.Time
	if !token.Expiry.IsZero() {
		expiry := token.Expiry
		expiresAt = &expiry
	}
	return Identity{
		ProviderUserID: idToken.Subject,
		Login:          login,
		Name:           name,
		AvatarURL:      claims.Picture,
		Token: Token{
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			ExpiresAt:    expiresAt,
		},
		Scopes: parseScopes(scope),
	}, nil
}

// Refresh renews an expiring user token when the provider issued a refresh
// token (offline_access).
func (p *OIDCProvider) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcDiscoveryTimeout)
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

// Teams returns the groups from the configured groups claim.
func (p *OIDCProvider) Teams(ctx context.Context, accessToken string) ([]Team, error) {
	info, err := p.userInfo(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	teams := make([]Team, 0, len(info.Groups))
	for _, group := range info.Groups {
		if group != "" {
			teams = append(teams, Team{Path: group})
		}
	}
	return teams, nil
}

func (p *OIDCProvider) Verify(ctx context.Context, accessToken string) error {
	info, err := p.userInfo(ctx, accessToken)
	if err != nil {
		return err
	}
	if info.Subject == "" {
		return fmt.Errorf("%w: userinfo has no subject", ErrProviderUnavailable)
	}
	return nil
}

func (p *OIDCProvider) userInfo(ctx context.Context, accessToken string) (oidcClaims, error) {
	if p.userInfoURL == "" {
		return oidcClaims{}, fmt.Errorf("%w: the provider has no userinfo endpoint", ErrProviderUnavailable)
	}
	var claims oidcClaims
	if err := p.getJSON(ctx, accessToken, p.userInfoURL, &claims); err != nil {
		return oidcClaims{}, err
	}
	return claims, nil
}

func (p *OIDCProvider) getJSON(ctx context.Context, accessToken, rawURL string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrTokenInvalid
	case resp.StatusCode == http.StatusForbidden:
		// Usually a missing scope or a userinfo endpoint that requires more
		// than read access: treat it as transient rather than disabling every
		// user on the next revalidation.
		return fmt.Errorf("%w: userinfo forbidden", ErrProviderUnavailable)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return fmt.Errorf("%w: status %d", ErrProviderUnavailable, resp.StatusCode)
	default:
		return fmt.Errorf("%w: status %d", ErrProviderUnavailable, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode OIDC userinfo: %w", err)
	}
	return nil
}

type oidcClaims struct {
	Subject           string   `json:"sub"`
	PreferredUsername string   `json:"preferred_username"`
	Name              string   `json:"name"`
	Email             string   `json:"email"`
	EmailVerified     bool     `json:"email_verified"`
	Picture           string   `json:"picture"`
	Groups            []string `json:"groups"`
}

// merge fills empty ID token claims with userinfo claims. Providers differ in
// which claims they put in each document.
func (c *oidcClaims) merge(other oidcClaims) {
	if c.Subject == "" {
		c.Subject = other.Subject
	}
	if c.PreferredUsername == "" {
		c.PreferredUsername = other.PreferredUsername
	}
	if c.Name == "" {
		c.Name = other.Name
	}
	if c.Email == "" {
		c.Email = other.Email
	}
	if !c.EmailVerified {
		c.EmailVerified = other.EmailVerified
	}
	if c.Picture == "" {
		c.Picture = other.Picture
	}
	if len(c.Groups) == 0 {
		c.Groups = other.Groups
	}
}

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// httpPolicy says how a provider's HTTP statuses map onto auth errors.
// Providers disagree on two of them, so each one states what it means instead
// of every provider carrying its own copy of the switch.
type httpPolicy struct {
	// NotFoundInvalid marks a 404 as a rejected token. APIs that answer an
	// unknown user or team with 404 use it; where a 404 means a wrong URL, it
	// has to stay transient.
	NotFoundInvalid bool
	// Forbidden maps a 403, which providers overload: a rate limit, a denied
	// user, or a missing scope.
	Forbidden func(resp *http.Response) error
}

// flow is the OAuth flow every provider shares: PKCE, the code exchange, the
// refresh, and the authenticated JSON GET that turns a status into an error.
type flow struct {
	config     *oauth2.Config
	client     *http.Client
	timeout    time.Duration
	headers    map[string]string
	authParams []oauth2.AuthCodeOption
	withNonce  bool
	policy     httpPolicy
}

func newFlow(config *oauth2.Config, headers map[string]string, policy httpPolicy) *flow {
	return &flow{
		config:  config,
		client:  &http.Client{Timeout: 10 * time.Second},
		timeout: 15 * time.Second,
		headers: headers,
		policy:  policy,
	}
}

func (f *flow) AuthCodeURL(state, nonce, verifier string) string {
	opts := make([]oauth2.AuthCodeOption, 0, len(f.authParams)+2)
	opts = append(opts, f.authParams...)
	if f.withNonce && nonce != "" {
		opts = append(opts, oauth2.SetAuthURLParam("nonce", nonce))
	}
	if verifier != "" {
		opts = append(opts, oauth2.S256ChallengeOption(verifier))
	}
	return f.config.AuthCodeURL(state, opts...)
}

func (f *flow) Exchange(ctx context.Context, code, verifier string) (*oauth2.Token, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	var opts []oauth2.AuthCodeOption
	if verifier != "" {
		opts = append(opts, oauth2.VerifierOption(verifier))
	}
	return f.config.Exchange(ctx, code, opts...)
}

// Refresh renews an expiring user token. Providers that rotate refresh tokens
// return a new one, which the caller must persist immediately: the one it
// replaces is invalidated as soon as this call succeeds.
func (f *flow) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	token, err := f.config.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken}).Token()
	if err != nil {
		return Token{}, refreshError(err)
	}
	return tokenFromOAuth(token), nil
}

func (f *flow) getJSON(ctx context.Context, accessToken, rawURL string, dst any) error {
	resp, err := f.get(ctx, accessToken, rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}

// get performs an authenticated GET and maps the response status onto the auth
// errors the revalidator acts on.
func (f *flow) get(ctx context.Context, accessToken, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	for key, value := range f.headers {
		req.Header.Set(key, value)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return resp, nil
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusNotFound && f.policy.NotFoundInvalid:
		_ = resp.Body.Close()
		return nil, ErrTokenInvalid
	case resp.StatusCode == http.StatusForbidden && f.policy.Forbidden != nil:
		_ = resp.Body.Close()
		return nil, f.policy.Forbidden(resp)
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: status %d", ErrProviderUnavailable, resp.StatusCode)
	}
}

// tokenFromOAuth converts an oauth2 token, keeping its expiry only when the
// provider set one.
func tokenFromOAuth(token *oauth2.Token) Token {
	converted := Token{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken}
	if !token.Expiry.IsZero() {
		expiry := token.Expiry
		converted.ExpiresAt = &expiry
	}
	return converted
}

// refreshError maps a failed token refresh. Only a rejected refresh token
// disables the user (they revoked the OAuth App, or the credential expired for
// good); anything else, including a misconfigured OAuth App, is transient so an
// outage does not disable every account.
func refreshError(err error) error {
	if retrieveErr, ok := errors.AsType[*oauth2.RetrieveError](err); ok {
		switch retrieveErr.ErrorCode {
		case "invalid_grant", "bad_refresh_token":
			return fmt.Errorf("%w: %s", ErrTokenInvalid, retrieveErr.ErrorCode)
		}
	}
	return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
}

// parseScopes splits the scope list a provider returns with a token.
func parseScopes(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' '
	})
}

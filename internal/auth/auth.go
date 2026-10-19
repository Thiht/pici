// Package auth authenticates humans through an external provider (GitHub for
// now) and issues the two credentials pici knows about: browser sessions for
// the UI, and personal API tokens for the CLI and scripts.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/stores"
)

const (
	SessionCookie       = "pici_session"
	OAuthStateCookie    = "pici_oauth_state"
	OAuthNextCookie     = "pici_oauth_next"
	OAuthVerifierCookie = "pici_oauth_verifier"

	// membershipCheckInterval is how often an active user is checked against
	// the configured teams. The revalidator runs more often than this and
	// skips users checked recently.
	membershipCheckInterval = 24 * time.Hour

	sessionTouchInterval = time.Hour
	tokenTouchInterval   = 5 * time.Minute
)

var (
	// ErrUnauthorized means the credential is missing, expired, or belongs to
	// a disabled user.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrAccessDenied means GitHub authenticated the user but they are not in
	// one of the configured teams, or did not grant the required scope.
	ErrAccessDenied = errors.New("access denied")
	// ErrTokenInvalid means the provider rejected the stored OAuth token:
	// the user revoked the app or their account is gone.
	ErrTokenInvalid = errors.New("oauth token invalid")
	// ErrProviderUnavailable covers network errors, rate limits and 5xx
	// responses. Access is kept when a revalidation hits it.
	ErrProviderUnavailable = errors.New("provider unavailable")
	// ErrExpiringTokens means the OAuth App is configured to issue expiring
	// user tokens. pici does not implement refresh tokens yet, so it refuses
	// to store a token that would silently expire.
	ErrExpiringTokens = errors.New("expiring OAuth tokens are not supported")
)

// Team identifies a GitHub team by organization and slug.
type Team struct {
	Org  string
	Slug string
}

// Identity is what a provider returns after a successful login.
type Identity struct {
	ProviderUserID string
	Login          string
	Name           string
	AvatarURL      string
	AccessToken    string
	Scopes         []string
}

// Provider is the minimal contract for an OAuth identity provider. Adding
// another provider later means implementing this interface and registering it
// in place of GitHub.
type Provider interface {
	Name() string
	AuthCodeURL(state, verifier string) string
	Exchange(ctx context.Context, code, verifier string) (Identity, error)
	// Teams returns the teams the user is a direct member of.
	Teams(ctx context.Context, accessToken string) ([]Team, error)
	// Verify checks that a stored access token is still valid. It is used for
	// allowlisted users when no teams are configured, so it must not require
	// the read:org scope.
	Verify(ctx context.Context, accessToken string) error
}

// Principal is the authenticated user attached to a request. Session is nil
// for API requests authenticated with a personal token.
type Principal struct {
	User    stores.User
	Session *stores.Session
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// UserID returns the authenticated user's id, or nil for anonymous requests.
func UserID(ctx context.Context) *uuid.UUID {
	p, ok := FromContext(ctx)
	if !ok {
		return nil
	}
	id := p.User.ID
	return &id
}

// Service owns login, browser sessions, personal tokens and the periodic team
// membership revalidation.
type Service struct {
	store      stores.Store
	provider   Provider
	teams      map[string]struct{}
	teamList   []string
	users      map[string]struct{}
	userList   []string
	sessionTTL time.Duration
	secure     bool
	now        func() time.Time
}

func NewService(store stores.Store, provider Provider, teams, users []string, sessionTTL time.Duration, secure bool) *Service {
	teamSet, teamList := normalizeAccessList(teams)
	userSet, userList := normalizeAccessList(users)
	return &Service{
		store:      store,
		provider:   provider,
		teams:      teamSet,
		teamList:   teamList,
		users:      userSet,
		userList:   userList,
		sessionTTL: sessionTTL,
		secure:     secure,
		now:        time.Now,
	}
}

func normalizeAccessList(values []string) (map[string]struct{}, []string) {
	set := make(map[string]struct{}, len(values))
	list := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		list = append(list, value)
		set[strings.ToLower(value)] = struct{}{}
	}
	return set, list
}

func (s *Service) Teams() []string           { return append([]string(nil), s.teamList...) }
func (s *Service) Users() []string           { return append([]string(nil), s.userList...) }
func (s *Service) SessionTTL() time.Duration { return s.sessionTTL }
func (s *Service) SecureCookies() bool       { return s.secure }
func (s *Service) LoginURL(state, verifier string) string {
	return s.provider.AuthCodeURL(state, verifier)
}

// Login exchanges an OAuth code, verifies team membership, and creates or
// refreshes the user. It does not create a browser session; the caller does
// that once it knows the user agent.
func (s *Service) Login(ctx context.Context, code, verifier string) (stores.User, error) {
	identity, err := s.provider.Exchange(ctx, code, verifier)
	if err != nil {
		return stores.User{}, fmt.Errorf("exchange oauth code: %w", err)
	}
	if len(s.teams) > 0 && !hasScope(identity.Scopes, "read:org") {
		return stores.User{}, fmt.Errorf("%w: the read:org scope was not granted", ErrAccessDenied)
	}
	allowed, err := s.authorized(ctx, identity.Login, identity.AccessToken)
	if err != nil {
		return stores.User{}, err
	}
	if !allowed {
		return stores.User{}, ErrAccessDenied
	}

	now := s.now()
	user, err := s.store.UpsertUser(ctx, stores.User{
		Provider:       s.provider.Name(),
		ProviderUserID: identity.ProviderUserID,
		Login:          identity.Login,
		Name:           identity.Name,
		AvatarURL:      identity.AvatarURL,
		OAuthToken:     identity.AccessToken,
		CreatedAt:      now,
		LastLoginAt:    now,
		LastOrgCheckAt: &now,
	})
	if err != nil {
		return stores.User{}, err
	}
	return user, nil
}

// authorized accepts a user who is explicitly allowlisted, or who belongs to
// one of the configured teams. When no team is configured, the provider only
// verifies the token, which keeps personal instances free of the read:org
// scope and of any GitHub organization.
func (s *Service) authorized(ctx context.Context, login, accessToken string) (bool, error) {
	if len(s.teams) == 0 {
		if err := s.provider.Verify(ctx, accessToken); err != nil {
			return false, err
		}
		return s.hasUser(login), nil
	}

	teams, err := s.provider.Teams(ctx, accessToken)
	if err != nil {
		return false, err
	}
	if s.hasUser(login) {
		return true, nil
	}
	for _, team := range teams {
		key := strings.ToLower(team.Org + "/" + team.Slug)
		if _, ok := s.teams[key]; ok {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) hasUser(login string) bool {
	_, ok := s.users[strings.ToLower(login)]
	return ok
}

// NewState returns an unguessable OAuth state value.
func NewState() (string, error) {
	return randomToken("")
}

// NewVerifier returns a PKCE code verifier (43 base64url characters, within
// RFC 7636's 43-128 range).
func NewVerifier() (string, error) {
	return randomToken("")
}

// CreateSession issues a browser session and returns the raw cookie value. The
// store only ever sees its hash.
func (s *Service) CreateSession(ctx context.Context, userID uuid.UUID, userAgent string) (string, stores.Session, error) {
	raw, err := randomToken("pici_sess_")
	if err != nil {
		return "", stores.Session{}, err
	}
	csrf, err := randomToken("")
	if err != nil {
		return "", stores.Session{}, err
	}
	if runes := []rune(userAgent); len(runes) > 512 {
		userAgent = string(runes[:512])
	}
	now := s.now()
	session := stores.Session{
		ID:         hashToken(raw),
		UserID:     userID,
		CSRFToken:  csrf,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(s.sessionTTL),
		UserAgent:  userAgent,
	}
	if err := s.store.CreateSession(ctx, session); err != nil {
		return "", stores.Session{}, err
	}
	return raw, session, nil
}

// AuthenticateSession resolves a session cookie and slides its expiry forward.
func (s *Service) AuthenticateSession(ctx context.Context, raw string) (stores.User, stores.Session, error) {
	if raw == "" {
		return stores.User{}, stores.Session{}, ErrUnauthorized
	}
	session, err := s.store.GetSession(ctx, hashToken(raw))
	if errors.Is(err, stores.ErrNotFound) {
		return stores.User{}, stores.Session{}, ErrUnauthorized
	}
	if err != nil {
		return stores.User{}, stores.Session{}, err
	}

	now := s.now()
	if !now.Before(session.ExpiresAt) {
		_ = s.store.DeleteSession(ctx, session.ID)
		return stores.User{}, stores.Session{}, ErrUnauthorized
	}

	user, err := s.store.GetUser(ctx, session.UserID)
	if errors.Is(err, stores.ErrNotFound) {
		return stores.User{}, stores.Session{}, ErrUnauthorized
	}
	if err != nil {
		return stores.User{}, stores.Session{}, err
	}
	if user.DisabledAt != nil {
		_ = s.store.DeleteUserSessions(ctx, user.ID)
		return stores.User{}, stores.Session{}, ErrUnauthorized
	}

	if now.Sub(session.LastSeenAt) >= sessionTouchInterval {
		expires := now.Add(s.sessionTTL)
		if err := s.store.TouchSession(ctx, session.ID, now, expires); err == nil {
			session.LastSeenAt = now
			session.ExpiresAt = expires
		}
	}
	return user, session, nil
}

func (s *Service) DeleteSessionForUser(ctx context.Context, userID uuid.UUID, id string) error {
	return s.store.DeleteUserSession(ctx, userID, id)
}

func (s *Service) ListSessions(ctx context.Context, userID uuid.UUID) ([]stores.Session, error) {
	return s.store.ListUserSessions(ctx, userID)
}

func (s *Service) DeleteAllSessions(ctx context.Context, userID uuid.UUID) error {
	return s.store.DeleteUserSessions(ctx, userID)
}

// CreateAPIToken issues a personal token. The raw value is never stored.
func (s *Service) CreateAPIToken(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time) (string, stores.APIToken, error) {
	raw, err := randomToken("pici_pat_")
	if err != nil {
		return "", stores.APIToken{}, err
	}
	now := s.now()
	token := stores.APIToken{
		ID:        uuid.New(),
		UserID:    userID,
		Name:      name,
		Prefix:    displayPrefix(raw),
		TokenHash: hashToken(raw),
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}
	if err := s.store.CreateAPIToken(ctx, token); err != nil {
		return "", stores.APIToken{}, err
	}
	return raw, token, nil
}

func (s *Service) AuthenticateToken(ctx context.Context, raw string) (stores.User, error) {
	if raw == "" {
		return stores.User{}, ErrUnauthorized
	}
	token, err := s.store.GetAPITokenByHash(ctx, hashToken(raw))
	if errors.Is(err, stores.ErrNotFound) {
		return stores.User{}, ErrUnauthorized
	}
	if err != nil {
		return stores.User{}, err
	}

	now := s.now()
	if token.ExpiresAt != nil && !now.Before(*token.ExpiresAt) {
		return stores.User{}, ErrUnauthorized
	}

	user, err := s.store.GetUser(ctx, token.UserID)
	if errors.Is(err, stores.ErrNotFound) {
		return stores.User{}, ErrUnauthorized
	}
	if err != nil {
		return stores.User{}, err
	}
	if user.DisabledAt != nil {
		return stores.User{}, ErrUnauthorized
	}

	if token.LastUsedAt == nil || now.Sub(*token.LastUsedAt) >= tokenTouchInterval {
		_ = s.store.TouchAPIToken(ctx, token.ID, now)
	}
	return user, nil
}

func (s *Service) ListAPITokens(ctx context.Context, userID uuid.UUID) ([]stores.APIToken, error) {
	return s.store.ListAPITokens(ctx, userID)
}

func (s *Service) RenameAPIToken(ctx context.Context, userID, id uuid.UUID, name string) error {
	return s.store.RenameAPIToken(ctx, userID, id, name)
}

func (s *Service) DeleteAPIToken(ctx context.Context, userID, id uuid.UUID) error {
	return s.store.DeleteAPIToken(ctx, userID, id)
}

// RunRevalidator checks team membership at login time and then periodically.
// The ticker runs often; each user is checked at most once per
// membershipCheckInterval.
func (s *Service) RunRevalidator(ctx context.Context, interval time.Duration) {
	s.Revalidate(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Revalidate(ctx)
		}
	}
}

// Revalidate disables users who left all configured teams or whose stored
// OAuth token was revoked. Transient provider failures keep access.
func (s *Service) Revalidate(ctx context.Context) {
	users, err := s.store.ListActiveUsers(ctx)
	if err != nil {
		slog.Error("list users for revalidation", "error", err)
		return
	}
	now := s.now()
	for _, user := range users {
		if user.LastOrgCheckAt != nil && now.Sub(*user.LastOrgCheckAt) < membershipCheckInterval {
			continue
		}
		allowed, err := s.authorized(ctx, user.Login, user.OAuthToken)
		switch {
		case err == nil && allowed:
			if err := s.store.SetUserOrgCheck(ctx, user.ID, now); err != nil {
				slog.Warn("update user org check", "login", user.Login, "error", err)
			}
		case err == nil && !allowed:
			s.disable(ctx, user, "no longer allowed by the configured users or teams")
		case errors.Is(err, ErrTokenInvalid):
			s.disable(ctx, user, "stored OAuth token is no longer valid")
		default:
			slog.Warn("team revalidation failed; keeping access", "login", user.Login, "error", err)
		}
	}
	if err := s.store.DeleteExpiredSessions(ctx, now); err != nil {
		slog.Warn("delete expired sessions", "error", err)
	}
}

func (s *Service) disable(ctx context.Context, user stores.User, reason string) {
	if err := s.store.DisableUser(ctx, user.ID, s.now()); err != nil {
		slog.Error("disable user", "login", user.Login, "error", err)
		return
	}
	if err := s.store.DeleteUserSessions(ctx, user.ID); err != nil {
		slog.Warn("delete disabled user sessions", "login", user.Login, "error", err)
	}
	slog.Warn("user disabled", "login", user.Login, "reason", reason)
}

func hasScope(scopes []string, want string) bool {
	for _, scope := range scopes {
		if scope == want {
			return true
		}
	}
	return false
}

func randomToken(prefix string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func displayPrefix(raw string) string {
	const n = len("pici_pat_") + 6
	if len(raw) < n {
		return raw
	}
	return raw[:n]
}

package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/auth/authtest"
	"github.com/Thiht/pici/internal/stores"
)

func newTestService(t *testing.T) (*auth.Service, stores.Store, *authtest.Provider) {
	t.Helper()
	return newTestServiceWithAccess(t, []string{"acme/pici"}, nil)
}

func newTestServiceWithAccess(t *testing.T, teams, users []string) (*auth.Service, stores.Store, *authtest.Provider) {
	t.Helper()
	store, err := stores.Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	provider := &authtest.Provider{
		Identity: auth.Identity{
			ProviderUserID: "1",
			Login:          "alice",
			Name:           "Alice",
			Token:          auth.Token{AccessToken: "oauth"},
			Scopes:         []string{"read:org"},
		},
		TeamList: []auth.Team{{Path: "acme/pici"}},
	}
	svc := auth.NewService(store, provider, teams, users, 24*time.Hour, false)
	return svc, store, provider
}

func TestLoginAndSession(t *testing.T) {
	svc, store, provider := newTestService(t)
	ctx := context.Background()

	user, err := svc.Login(ctx, "code", "nonce", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if provider.LastVerifier != "verifier" {
		t.Fatalf("PKCE verifier not forwarded: %q", provider.LastVerifier)
	}
	if user.Login != "alice" || user.OAuthAccessToken != "oauth" || user.DisabledAt != nil {
		t.Fatalf("unexpected user: %+v", user)
	}

	raw, session, err := svc.CreateSession(ctx, user.ID, "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" || session.ID == raw || session.CSRFToken == "" {
		t.Fatalf("unexpected session: raw=%q %+v", raw, session)
	}

	gotUser, gotSession, err := svc.AuthenticateSession(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if gotUser.ID != user.ID || gotSession.ID != session.ID {
		t.Fatalf("unexpected auth: %+v %+v", gotUser, gotSession)
	}

	stored, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != hashToken(raw) {
		t.Fatalf("session id is not the token hash")
	}
}

func TestLoginStoresExpiringToken(t *testing.T) {
	svc, store, provider := newTestService(t)
	expires := time.Now().Add(8 * time.Hour)
	provider.Identity.Token = auth.Token{AccessToken: "gho_test", RefreshToken: "ghr_test", ExpiresAt: &expires}

	user, err := svc.Login(context.Background(), "code", "nonce", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if user.OAuthAccessToken != "gho_test" || user.OAuthRefreshToken != "ghr_test" {
		t.Fatalf("unexpected credentials: %+v", user)
	}
	if user.OAuthExpiresAt == nil || user.OAuthExpiresAt.Unix() != expires.Unix() {
		t.Fatalf("unexpected expiry: %v", user.OAuthExpiresAt)
	}

	got, err := store.GetUser(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OAuthRefreshToken != "ghr_test" || got.OAuthExpiresAt == nil {
		t.Fatalf("credentials not persisted: %+v", got)
	}
}

func TestLoginAccessDenied(t *testing.T) {
	t.Run("no team", func(t *testing.T) {
		svc, _, provider := newTestService(t)
		provider.TeamList = nil
		if _, err := svc.Login(context.Background(), "code", "nonce", "verifier"); !errors.Is(err, auth.ErrAccessDenied) {
			t.Fatalf("expected auth.ErrAccessDenied, got %v", err)
		}
	})
	t.Run("missing scope", func(t *testing.T) {
		svc, _, provider := newTestService(t)
		provider.Identity.Scopes = []string{"repo"}
		if _, err := svc.Login(context.Background(), "code", "nonce", "verifier"); !errors.Is(err, auth.ErrAccessDenied) {
			t.Fatalf("expected auth.ErrAccessDenied, got %v", err)
		}
	})
}

func TestLoginAllowlistedUserWithoutTeams(t *testing.T) {
	svc, _, provider := newTestServiceWithAccess(t, nil, []string{"alice"})
	provider.Identity.Scopes = nil

	user, err := svc.Login(context.Background(), "code", "nonce", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if user.Login != "alice" {
		t.Fatalf("unexpected user: %+v", user)
	}
	if provider.VerifyCalls != 1 || provider.TeamsCalls != 0 {
		t.Fatalf("expected a token verification without team lookup, got verify=%d teams=%d", provider.VerifyCalls, provider.TeamsCalls)
	}
}

func TestLoginDeniesUserNotAllowlistedWithoutTeams(t *testing.T) {
	svc, _, provider := newTestServiceWithAccess(t, nil, []string{"bob"})
	provider.Identity.Scopes = nil

	_, err := svc.Login(context.Background(), "code", "nonce", "verifier")
	if !errors.Is(err, auth.ErrAccessDenied) {
		t.Fatalf("expected auth.ErrAccessDenied, got %v", err)
	}
	if provider.VerifyCalls != 1 {
		t.Fatalf("expected the token to be verified, got %d calls", provider.VerifyCalls)
	}
}

func TestMixedModeAllowsListedUser(t *testing.T) {
	svc, _, provider := newTestServiceWithAccess(t, []string{"acme/other"}, []string{"alice"})
	provider.TeamList = nil

	if _, err := svc.Login(context.Background(), "code", "nonce", "verifier"); err != nil {
		t.Fatalf("allowlisted user should be allowed: %v", err)
	}
	if provider.TeamsCalls != 1 {
		t.Fatalf("expected teams to be checked in mixed mode, got %d calls", provider.TeamsCalls)
	}
}

func TestLoginProviderError(t *testing.T) {
	svc, _, provider := newTestService(t)
	provider.ExchangeErr = errors.New("boom")
	_, err := svc.Login(context.Background(), "code", "nonce", "verifier")
	if err == nil || errors.Is(err, auth.ErrAccessDenied) {
		t.Fatalf("expected provider error, got %v", err)
	}
}

func TestExpiredSession(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	user := seedUser(t, store)
	raw, session := createSession(t, svc, user.ID)

	if err := store.TouchSession(ctx, session.ID, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AuthenticateSession(ctx, raw); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("expected auth.ErrUnauthorized, got %v", err)
	}
	if _, err := store.GetSession(ctx, session.ID); !errors.Is(err, stores.ErrNotFound) {
		t.Fatalf("expected expired session to be deleted, got %v", err)
	}
}

func TestDisabledUserSession(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	user := seedUser(t, store)
	raw, session := createSession(t, svc, user.ID)

	if err := store.DisableUser(ctx, user.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AuthenticateSession(ctx, raw); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("expected auth.ErrUnauthorized, got %v", err)
	}
	if _, err := store.GetSession(ctx, session.ID); !errors.Is(err, stores.ErrNotFound) {
		t.Fatalf("expected disabled user sessions to be deleted, got %v", err)
	}
}

func TestAuthenticateToken(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	user, err := svc.Login(ctx, "code", "nonce", "verifier")
	if err != nil {
		t.Fatal(err)
	}

	raw, token, err := svc.CreateAPIToken(ctx, user.ID, "cli", nil)
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" || token.TokenHash == raw || token.Prefix == "" {
		t.Fatalf("unexpected token: raw=%q %+v", raw, token)
	}

	got, err := svc.AuthenticateToken(ctx, raw)
	if err != nil || got.ID != user.ID {
		t.Fatalf("authenticate token: %+v (err=%v)", got, err)
	}
	if _, err := svc.AuthenticateToken(ctx, "wrong"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("expected auth.ErrUnauthorized, got %v", err)
	}

	past := time.Now().Add(-time.Hour)
	rawExpired, _, err := svc.CreateAPIToken(ctx, user.ID, "expired", &past)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateToken(ctx, rawExpired); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("expected expired token to be unauthorized, got %v", err)
	}

	if err := store.DisableUser(ctx, user.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateToken(ctx, raw); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("expected disabled user token to be unauthorized, got %v", err)
	}
}

func TestRevalidateAllowlistedUser(t *testing.T) {
	svc, store, provider := newTestServiceWithAccess(t, nil, []string{"alice"})
	ctx := context.Background()
	user := seedUser(t, store)

	svc.Revalidate(ctx)

	got, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisabledAt != nil {
		t.Fatal("allowlisted user should stay active")
	}
	if got.LastOrgCheckAt == nil {
		t.Fatal("expected last_org_check_at to be updated")
	}
	if provider.VerifyCalls != 1 || provider.TeamsCalls != 0 {
		t.Fatalf("expected a token verification without team lookup, got verify=%d teams=%d", provider.VerifyCalls, provider.TeamsCalls)
	}
}

func TestRevalidateRemovedAllowlistedUser(t *testing.T) {
	svc, store, _ := newTestServiceWithAccess(t, nil, nil)
	ctx := context.Background()
	user := seedUser(t, store)

	svc.Revalidate(ctx)

	got, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisabledAt == nil {
		t.Fatal("user removed from the allowlist should be disabled")
	}
}

func TestRevalidateKeepsMember(t *testing.T) {
	svc, store, provider := newTestService(t)
	ctx := context.Background()
	user := seedUser(t, store)
	provider.TeamList = []auth.Team{{Path: "acme/pici"}}

	svc.Revalidate(ctx)

	got, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisabledAt != nil {
		t.Fatal("member should stay active")
	}
	if got.LastOrgCheckAt == nil {
		t.Fatal("expected last_org_check_at to be updated")
	}
}

func TestRevalidateDisablesRemovedMember(t *testing.T) {
	svc, store, provider := newTestService(t)
	ctx := context.Background()
	user := seedUser(t, store)
	_, session := createSession(t, svc, user.ID)

	provider.TeamList = nil
	svc.Revalidate(ctx)

	got, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisabledAt == nil {
		t.Fatal("expected user to be disabled")
	}
	if _, err := store.GetSession(ctx, session.ID); !errors.Is(err, stores.ErrNotFound) {
		t.Fatalf("expected sessions to be deleted, got %v", err)
	}
}

func TestRevalidateKeepsAccessOnProviderFailure(t *testing.T) {
	svc, store, provider := newTestService(t)
	ctx := context.Background()
	user := seedUser(t, store)

	provider.TeamsErr = auth.ErrProviderUnavailable
	svc.Revalidate(ctx)

	got, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisabledAt != nil {
		t.Fatal("transient provider failure must not disable the user")
	}
}

func TestRevalidateDisablesInvalidToken(t *testing.T) {
	svc, store, provider := newTestService(t)
	ctx := context.Background()
	user := seedUser(t, store)

	provider.TeamsErr = auth.ErrTokenInvalid
	svc.Revalidate(ctx)

	got, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisabledAt == nil {
		t.Fatal("expected an invalid OAuth token to disable the user")
	}
}

func TestRevalidateSkipsRecentlyCheckedUsers(t *testing.T) {
	svc, store, provider := newTestService(t)
	ctx := context.Background()
	now := time.Now()
	user, err := store.UpsertUser(ctx, stores.User{
		ID:               uuid.New(),
		Provider:         "github",
		ProviderUserID:   "2",
		Login:            "bob",
		OAuthAccessToken: "oauth",
		CreatedAt:        now,
		LastLoginAt:      now,
		LastOrgCheckAt:   &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider.TeamList = nil
	svc.Revalidate(ctx)
	got, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisabledAt != nil {
		t.Fatal("user checked recently must not be revalidated yet")
	}
}

func TestRevalidateRefreshesExpiredToken(t *testing.T) {
	svc, store, provider := newTestService(t)
	ctx := context.Background()
	expired := time.Now().Add(-time.Hour)
	user, err := store.UpsertUser(ctx, stores.User{
		ID:                uuid.New(),
		Provider:          "github",
		ProviderUserID:    "2",
		Login:             "bob",
		OAuthAccessToken:  "expired",
		OAuthRefreshToken: "refresh-1",
		OAuthExpiresAt:    &expired,
		CreatedAt:         expired,
		LastLoginAt:       expired,
	})
	if err != nil {
		t.Fatal(err)
	}
	next := time.Now().Add(8 * time.Hour)
	provider.Refreshed = auth.Token{AccessToken: "fresh", RefreshToken: "refresh-2", ExpiresAt: &next}

	svc.Revalidate(ctx)

	if provider.RefreshCalls != 1 || provider.LastRefresh != "refresh-1" {
		t.Fatalf("expected one refresh of refresh-1, got %d calls with %q", provider.RefreshCalls, provider.LastRefresh)
	}
	if provider.LastToken != "fresh" {
		t.Fatalf("membership check used %q, want the refreshed token", provider.LastToken)
	}
	got, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OAuthAccessToken != "fresh" || got.OAuthRefreshToken != "refresh-2" {
		t.Fatalf("refreshed token not persisted: %+v", got)
	}
	if got.OAuthExpiresAt == nil || got.OAuthExpiresAt.Unix() != next.Unix() {
		t.Fatalf("unexpected stored expiry: %v", got.OAuthExpiresAt)
	}
	if got.DisabledAt != nil {
		t.Fatal("refreshing must keep the user active")
	}
}

func TestRevalidateKeepsTokenBeforeExpiry(t *testing.T) {
	svc, store, provider := newTestService(t)
	ctx := context.Background()
	valid := time.Now().Add(8 * time.Hour)
	user, err := store.UpsertUser(ctx, stores.User{
		ID:                uuid.New(),
		Provider:          "github",
		ProviderUserID:    "2",
		Login:             "bob",
		OAuthAccessToken:  "current",
		OAuthRefreshToken: "refresh-1",
		OAuthExpiresAt:    &valid,
		CreatedAt:         time.Now(),
		LastLoginAt:       time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	svc.Revalidate(ctx)

	if provider.RefreshCalls != 0 {
		t.Fatalf("expected no refresh before expiry, got %d", provider.RefreshCalls)
	}
	if provider.LastToken != "current" {
		t.Fatalf("membership check used %q, want the stored token", provider.LastToken)
	}
	got, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OAuthRefreshToken != "refresh-1" {
		t.Fatalf("refresh token changed: %+v", got)
	}
}

func TestRevalidateDisablesUserOnRejectedRefresh(t *testing.T) {
	svc, store, provider := newTestService(t)
	ctx := context.Background()
	expired := time.Now().Add(-time.Hour)
	user, err := store.UpsertUser(ctx, stores.User{
		ID:                uuid.New(),
		Provider:          "github",
		ProviderUserID:    "2",
		Login:             "bob",
		OAuthAccessToken:  "expired",
		OAuthRefreshToken: "refresh-1",
		OAuthExpiresAt:    &expired,
		CreatedAt:         expired,
		LastLoginAt:       expired,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider.RefreshErr = auth.ErrTokenInvalid

	svc.Revalidate(ctx)

	got, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisabledAt == nil {
		t.Fatal("a rejected refresh token must disable the user")
	}
	if provider.TeamsCalls != 0 {
		t.Fatal("a failed refresh must not run a membership check with a dead token")
	}
}

func TestRevalidateKeepsAccessOnRefreshFailure(t *testing.T) {
	svc, store, provider := newTestService(t)
	ctx := context.Background()
	expired := time.Now().Add(-time.Hour)
	user, err := store.UpsertUser(ctx, stores.User{
		ID:                uuid.New(),
		Provider:          "github",
		ProviderUserID:    "2",
		Login:             "bob",
		OAuthAccessToken:  "expired",
		OAuthRefreshToken: "refresh-1",
		OAuthExpiresAt:    &expired,
		CreatedAt:         expired,
		LastLoginAt:       expired,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider.RefreshErr = auth.ErrProviderUnavailable

	svc.Revalidate(ctx)

	got, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisabledAt != nil {
		t.Fatal("a transient refresh failure must not disable the user")
	}
	if got.OAuthRefreshToken != "refresh-1" {
		t.Fatalf("unexpected token change on failure: %+v", got)
	}
	if provider.TeamsCalls != 0 {
		t.Fatal("a failed refresh must not run a membership check with an expired token")
	}
}

func TestUsersFromAnotherProviderAreRejected(t *testing.T) {
	store, err := stores.Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	provider := &authtest.Provider{ProviderName: "gitlab", TeamList: []auth.Team{{Path: "acme/pici"}}}
	svc := auth.NewService(store, provider, []string{"acme/pici"}, nil, 24*time.Hour, false)
	ctx := context.Background()
	user := seedUser(t, store) // stored with provider "github"
	raw, session := createSession(t, svc, user.ID)

	if _, _, err := svc.AuthenticateSession(ctx, raw); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("expected the old provider session to be unauthorized, got %v", err)
	}
	if _, err := store.GetSession(ctx, session.ID); !errors.Is(err, stores.ErrNotFound) {
		t.Fatalf("expected the old provider session to be deleted, got %v", err)
	}

	rawToken, _, err := svc.CreateAPIToken(ctx, user.ID, "cli", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateToken(ctx, rawToken); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("expected the old provider token to be unauthorized, got %v", err)
	}

	svc.Revalidate(ctx)
	got, err := store.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisabledAt == nil {
		t.Fatal("expected a user from another provider to be disabled")
	}
}

func seedUser(t *testing.T, store stores.Store) stores.User {
	t.Helper()
	now := time.Now()
	user, err := store.UpsertUser(context.Background(), stores.User{
		ID:               uuid.New(),
		Provider:         "github",
		ProviderUserID:   "1",
		Login:            "alice",
		OAuthAccessToken: "oauth",
		CreatedAt:        now,
		LastLoginAt:      now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func createSession(t *testing.T, svc *auth.Service, userID uuid.UUID) (string, stores.Session) {
	t.Helper()
	raw, session, err := svc.CreateSession(context.Background(), userID, "test")
	if err != nil {
		t.Fatal(err)
	}
	return raw, session
}

// hashToken mirrors the session id the service derives from a raw token.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

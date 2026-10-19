package stores

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/secrets"
)

func TestUserUpsertAndEncryption(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	cipher, err := secrets.New(hex.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(testDriver(), dsn, cipher)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	now := time.Now().Add(-time.Hour)
	expires := now.Add(8 * time.Hour)
	user, err := s.UpsertUser(ctx, User{
		ID:                uuid.New(),
		Provider:          "github",
		ProviderUserID:    "42",
		Login:             "alice",
		Name:              "Alice",
		OAuthAccessToken:  "oauth-secret",
		OAuthRefreshToken: "refresh-secret",
		OAuthExpiresAt:    &expires,
		CreatedAt:         now,
		LastLoginAt:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.OAuthAccessToken != "oauth-secret" || user.OAuthRefreshToken != "refresh-secret" || user.OAuthExpiresAt == nil {
		t.Fatalf("expected decrypted credentials, got %+v", user)
	}

	raw := rawTestDB(t, dsn)
	var stored, storedRefresh string
	if err := raw.QueryRowContext(ctx, `SELECT oauth_token, oauth_refresh_token FROM users WHERE provider_user_id = '42'`).Scan(&stored, &storedRefresh); err != nil {
		t.Fatal(err)
	}
	if stored == "oauth-secret" || stored == "" {
		t.Fatalf("OAuth token stored in plaintext: %q", stored)
	}
	if storedRefresh == "refresh-secret" || storedRefresh == "" {
		t.Fatalf("OAuth refresh token stored in plaintext: %q", storedRefresh)
	}

	// A second login for the same provider id updates the profile, keeps the
	// row id and created_at, and clears a previous disable.
	later := time.Now()
	if err := s.DisableUser(ctx, user.ID, later); err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpsertUser(ctx, User{
		ID:                uuid.New(),
		Provider:          "github",
		ProviderUserID:    "42",
		Login:             "alice-renamed",
		OAuthAccessToken:  "new-secret",
		OAuthRefreshToken: "new-refresh",
		CreatedAt:         later,
		LastLoginAt:       later,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != user.ID || updated.Login != "alice-renamed" || updated.OAuthAccessToken != "new-secret" {
		t.Fatalf("unexpected updated user: %+v", updated)
	}
	if updated.OAuthRefreshToken != "new-refresh" || updated.OAuthExpiresAt != nil {
		t.Fatalf("refresh token not rotated on login: %+v", updated)
	}
	if updated.DisabledAt != nil {
		t.Fatal("login should reactivate a disabled user")
	}
	if updated.CreatedAt.Unix() != user.CreatedAt.Unix() {
		t.Fatalf("created_at changed: %v -> %v", user.CreatedAt, updated.CreatedAt)
	}

	if err := s.DisableUser(ctx, user.ID, later); err != nil {
		t.Fatal(err)
	}
	disabled, err := s.GetUser(ctx, user.ID)
	if err != nil || disabled.DisabledAt == nil {
		t.Fatalf("expected disabled user, got %+v (err=%v)", disabled, err)
	}
	active, err := s.ListActiveUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("expected no active users, got %d", len(active))
	}
}

func TestUpdateUserOAuthToken(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	cipher, err := secrets.New(hex.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(testDriver(), dsn, cipher)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	user := mustUser(t, s, "42", "alice")

	expires := time.Now().Add(8 * time.Hour).Truncate(time.Millisecond)
	if err := s.UpdateUserOAuthToken(ctx, user.ID, "fresh", "refresh-2", &expires); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OAuthAccessToken != "fresh" || got.OAuthRefreshToken != "refresh-2" {
		t.Fatalf("unexpected credentials: %+v", got)
	}
	if got.OAuthExpiresAt == nil || !got.OAuthExpiresAt.Equal(expires) {
		t.Fatalf("unexpected expiry: %v", got.OAuthExpiresAt)
	}

	var stored string
	if err := rawTestDB(t, dsn).QueryRowContext(ctx, `SELECT oauth_refresh_token FROM users WHERE provider_user_id = '42'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "refresh-2" || stored == "" {
		t.Fatalf("refresh token stored in plaintext: %q", stored)
	}

	// Providers that stop returning an expiry leave the column NULL.
	if err := s.UpdateUserOAuthToken(ctx, user.ID, "fresh", "refresh-2", nil); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OAuthExpiresAt != nil {
		t.Fatalf("expected no expiry, got %v", got.OAuthExpiresAt)
	}
}

func TestSessionCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	user := mustUser(t, s, "42", "alice")
	other := mustUser(t, s, "43", "bob")

	now := time.Now().Truncate(time.Millisecond)
	session := Session{
		ID:         "hash-1",
		UserID:     user.ID,
		CSRFToken:  "csrf",
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(time.Hour),
		UserAgent:  "test-agent",
	}
	if err := s.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(ctx, session.ID)
	if err != nil || got.UserID != user.ID || got.CSRFToken != "csrf" || got.UserAgent != "test-agent" {
		t.Fatalf("unexpected session: %+v (err=%v)", got, err)
	}

	seen := now.Add(30 * time.Minute)
	expires := now.Add(2 * time.Hour)
	if err := s.TouchSession(ctx, session.ID, seen, expires); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetSession(ctx, session.ID)
	if err != nil || got.LastSeenAt.UnixMilli() != seen.UnixMilli() || got.ExpiresAt.UnixMilli() != expires.UnixMilli() {
		t.Fatalf("touch not applied: %+v (err=%v)", got, err)
	}

	list, err := s.ListUserSessions(ctx, user.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list sessions: %d (err=%v)", len(list), err)
	}

	// A user cannot delete someone else's session.
	if err := s.DeleteUserSession(ctx, other.ID, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, session.ID); err != nil {
		t.Fatalf("session should still exist: %v", err)
	}
	if err := s.DeleteUserSession(ctx, user.ID, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected session to be deleted, got %v", err)
	}

	expired := Session{ID: "hash-2", UserID: user.ID, CSRFToken: "csrf", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(-time.Minute)}
	if err := s.CreateSession(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteExpiredSessions(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, expired.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected expired session to be deleted, got %v", err)
	}
}

func TestAPITokenCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	user := mustUser(t, s, "42", "alice")
	other := mustUser(t, s, "43", "bob")

	now := time.Now().Truncate(time.Millisecond)
	expires := now.Add(90 * 24 * time.Hour)
	token := APIToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		Name:      "cli",
		Prefix:    "pici_pat_abc",
		TokenHash: "hash-1",
		CreatedAt: now,
		ExpiresAt: &expires,
	}
	if err := s.CreateAPIToken(ctx, token); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAPITokenByHash(ctx, "hash-1")
	if err != nil || got.ID != token.ID || got.UserID != user.ID || got.ExpiresAt == nil {
		t.Fatalf("unexpected token: %+v (err=%v)", got, err)
	}
	if _, err := s.GetAPITokenByHash(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	used := now.Add(time.Minute)
	if err := s.TouchAPIToken(ctx, token.ID, used); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListAPITokens(ctx, user.ID)
	if err != nil || len(list) != 1 || list[0].LastUsedAt == nil || list[0].LastUsedAt.UnixMilli() != used.UnixMilli() {
		t.Fatalf("list tokens: %+v (err=%v)", list, err)
	}

	if err := s.RenameAPIToken(ctx, user.ID, token.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListAPITokens(ctx, user.ID)
	if err != nil || len(list) != 1 || list[0].Name != "renamed" {
		t.Fatalf("rename not applied: %+v (err=%v)", list, err)
	}

	if err := s.DeleteAPIToken(ctx, other.ID, token.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAPITokenByHash(ctx, "hash-1"); err != nil {
		t.Fatalf("token should still exist: %v", err)
	}
	if err := s.DeleteAPIToken(ctx, user.ID, token.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAPITokenByHash(ctx, "hash-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected token to be deleted, got %v", err)
	}
}

func TestExecutionCreatedBy(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	project := mustProject(t, s)
	user := mustUser(t, s, "42", "alice")

	exec := Execution{
		ProjectID: project.ID,
		Workflow:  "build",
		Ref:       "main",
		Status:    StatusPending,
		Trigger:   TriggerManual,
		Source:    SourceGit,
		CreatedAt: time.Now(),
		CreatedBy: &user.ID,
	}
	if err := s.CreateExecution(ctx, &exec); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetExecution(ctx, project.ID, exec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CreatedBy == nil || *got.CreatedBy != user.ID || got.CreatedByLogin != "alice" {
		t.Fatalf("unexpected attribution: %+v", got)
	}

	list, err := s.ListExecutions(ctx, project.ID, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list executions: %d (err=%v)", len(list), err)
	}
	if list[0].CreatedByLogin != "alice" {
		t.Fatalf("missing attribution in list: %+v", list[0])
	}
}

func mustUser(t *testing.T, s Store, providerID, login string) User {
	t.Helper()
	now := time.Now()
	user, err := s.UpsertUser(context.Background(), User{
		ID:             uuid.New(),
		Provider:       "github",
		ProviderUserID: providerID,
		Login:          login,
		CreatedAt:      now,
		LastLoginAt:    now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func mustProject(t *testing.T, s Store) Project {
	t.Helper()
	now := time.Now()
	project, err := s.CreateProject(context.Background(), Project{
		ID:        uuid.New(),
		Name:      "demo",
		RepoURL:   "https://github.com/acme/demo.git",
		Provider:  ProviderGithub,
		AuthType:  AuthTypeNone,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return project
}

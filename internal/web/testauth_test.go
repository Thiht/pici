package web

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/auth/authtest"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
)

func fakeIdentity() auth.Identity {
	return auth.Identity{
		ProviderUserID: "1",
		Login:          "alice",
		Name:           "Alice",
		Token:          auth.Token{AccessToken: "oauth"},
		Scopes:         []string{"read:org"},
	}
}

func newWebTestStore(t *testing.T) stores.Store {
	t.Helper()
	store, err := stores.Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newWebTestUser(t *testing.T, store stores.Store) stores.User {
	t.Helper()
	now := time.Now()
	user, err := store.UpsertUser(context.Background(), stores.User{
		ID:               uuid.New(),
		Provider:         "github",
		ProviderUserID:   "1",
		Login:            "alice",
		Name:             "Alice",
		OAuthAccessToken: "oauth",
		CreatedAt:        now,
		LastLoginAt:      now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func newWebTestService(t *testing.T, store stores.Store, provider auth.Provider, teams, users []string) *auth.Service {
	t.Helper()
	return auth.NewService(store, provider, teams, users, 30*24*time.Hour, false)
}

func newWebTestSession(t *testing.T, svc *auth.Service, userID uuid.UUID) (string, stores.Session) {
	t.Helper()
	raw, session, err := svc.CreateSession(context.Background(), userID, "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	return raw, session
}

func newCookieClient(t *testing.T, rawURL, sessionRaw string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	if sessionRaw != "" {
		jar.SetCookies(u, []*http.Cookie{{Name: auth.SessionCookie, Value: sessionRaw, Path: "/"}})
	}
	return &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type testEnv struct {
	srv        *httptest.Server
	client     *http.Client
	store      stores.Store
	svc        *auth.Service
	provider   *authtest.Provider
	user       stores.User
	session    stores.Session
	sessionRaw string
}

func newTestEnv(t *testing.T, opts ...func(*authtest.Provider)) *testEnv {
	t.Helper()
	return newTestEnvWithAccess(t, []string{"acme/pici"}, nil, opts...)
}

func newTestEnvWithAccess(t *testing.T, teams, users []string, opts ...func(*authtest.Provider)) *testEnv {
	t.Helper()
	store := newWebTestStore(t)
	provider := &authtest.Provider{
		Identity: fakeIdentity(),
		TeamList: []auth.Team{{Path: "acme/pici"}},
	}
	for _, opt := range opts {
		opt(provider)
	}
	svc := newWebTestService(t, store, provider, teams, users)
	user := newWebTestUser(t, store)
	raw, session := newWebTestSession(t, svc, user.ID)

	h := newTestHandler(store, &ci.Runner{Store: store}, t.TempDir(), svc, "https://pici.test", "dev")
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	registerTestCSRF(srv.URL, session.CSRFToken)

	return &testEnv{
		srv:        srv,
		client:     newCookieClient(t, srv.URL, raw),
		store:      store,
		svc:        svc,
		provider:   provider,
		user:       user,
		session:    session,
		sessionRaw: raw,
	}
}

func (e *testEnv) anonymousClient(t *testing.T) *http.Client {
	t.Helper()
	return newCookieClient(t, e.srv.URL, "")
}

var testCSRFTokens sync.Map

func registerTestCSRF(rawURL, token string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	testCSRFTokens.Store(u.Scheme+"://"+u.Host, token)
}

func lookupTestCSRF(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	value, ok := testCSRFTokens.Load(u.Scheme + "://" + u.Host)
	if !ok {
		return ""
	}
	return value.(string)
}

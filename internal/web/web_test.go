package web

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/Thiht/pici/internal/app/artifacts"
	"github.com/Thiht/pici/internal/app/cache"
	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

func newTestServer(t *testing.T, _ string) (*httptest.Server, *http.Client) {
	t.Helper()
	env := newTestEnv(t)
	return env.srv, env.client
}

func postForm(t *testing.T, client *http.Client, rawURL string, values url.Values) *http.Response {
	t.Helper()
	if values == nil {
		values = url.Values{}
	}
	if values.Get("_csrf") == "" {
		if token := lookupTestCSRF(rawURL); token != "" {
			values.Set("_csrf", token)
		}
	}
	resp, err := client.PostForm(rawURL, values)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func cookieValue(client *http.Client, rawURL, name string) string {
	u, _ := url.Parse(rawURL)
	for _, c := range client.Jar.Cookies(u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func TestTemplatesParse(t *testing.T) {
	for _, name := range []string{"head", "nav", "foot", "flash", "login", "account", "projects_list", "project_form", "project_fields", "secret_fields", "secret_fields_inner", "project_detect", "webhook_help", "webhook_help_inner", "webhook_payload_url", "project_show", "project_variables", "project_cache", "workflows", "refs", "variables_list", "execution_show", "error"} {
		if templates.Lookup(name) == nil {
			t.Errorf("template %q not found", name)
		}
	}
}

func TestProjectsListEmpty(t *testing.T) {
	srv, client := newTestServer(t, "")
	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(body(t, resp), "No projects yet") {
		t.Fatal("expected empty state")
	}
}

func TestProjectCreateAndShow(t *testing.T) {
	srv, client := newTestServer(t, "")
	resp := postForm(t, client, srv.URL+"/projects", url.Values{
		"name":           {"demo"},
		"repo_url":       {"https://github.com/acme/demo.git"},
		"provider":       {"github"},
		"auth_type":      {"none"},
		"default_branch": {"main"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", resp.StatusCode, body(t, resp))
	}
	location := resp.Header.Get("Location")
	if !strings.HasPrefix(location, "/projects/") {
		t.Fatalf("unexpected redirect: %q", location)
	}
	shown, err := client.Get(srv.URL + location)
	if err != nil {
		t.Fatal(err)
	}
	if shown.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", shown.StatusCode)
	}
	if !strings.Contains(body(t, shown), "demo") {
		t.Fatal("expected project name in page")
	}
}

func TestProjectCreateValidation(t *testing.T) {
	srv, client := newTestServer(t, "")
	resp := postForm(t, client, srv.URL+"/projects", url.Values{"name": {""}, "repo_url": {""}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if !strings.Contains(body(t, resp), "Name is required") {
		t.Fatal("expected validation message")
	}
}

func TestGlobalVariableSetAndDelete(t *testing.T) {
	srv, client := newTestServer(t, "")
	resp := postForm(t, client, srv.URL+"/variables", url.Values{"key": {"TOKEN"}, "value": {"abc"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	list, err := client.Get(srv.URL + "/variables")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body(t, list), "TOKEN") {
		t.Fatal("expected variable in list")
	}
	del := postForm(t, client, srv.URL+"/variables/TOKEN/delete", url.Values{})
	if del.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on delete, got %d", del.StatusCode)
	}
}

func TestProjectVariablesShowInheritedGlobals(t *testing.T) {
	srv, client := newTestServer(t, "")
	created := postForm(t, client, srv.URL+"/projects", url.Values{
		"name":           {"demo"},
		"repo_url":       {"https://github.com/acme/demo.git"},
		"provider":       {"github"},
		"auth_type":      {"none"},
		"default_branch": {"main"},
	})
	if created.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", created.StatusCode, body(t, created))
	}
	project := created.Header.Get("Location")

	if resp := postForm(t, client, srv.URL+"/variables", url.Values{"key": {"SHARED"}, "value": {"global"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if resp := postForm(t, client, srv.URL+"/variables", url.Values{"key": {"GLOBAL_ONLY"}, "value": {"on"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if resp := postForm(t, client, srv.URL+project+"/variables", url.Values{"key": {"SHARED"}, "value": {"project"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}

	resp, err := client.Get(srv.URL + project + "/variables")
	if err != nil {
		t.Fatal(err)
	}
	out := body(t, resp)
	for _, want := range []string{"Inherited from instance", "GLOBAL_ONLY", "SHARED", "Overridden"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in project variables page", want)
		}
	}
}

func TestFlashRendersKind(t *testing.T) {
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "flash", base{FlashKind: "success", FlashText: "Image deleted."}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"alert-soft", "alert-success", "Image deleted."} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in flash, got:\n%s", want, out)
		}
	}
}

func TestExecutionTemplateHidesZeroExitCode(t *testing.T) {
	started := time.Now()
	finished := started.Add(time.Second)
	data := executionPage{
		Title: "Execution", Active: "projects", Version: "dev",
		Execution: stores.Execution{
			ExecutionID: 1, Attempt: 1, AttemptsCount: 1,
			Workflow: "build", Ref: "main", Status: stores.StatusFailed,
			CreatedAt: started, AttemptCreatedAt: started, StartedAt: &started, FinishedAt: &finished,
		},
		Steps: []stepView{
			{Name: "ok", Status: stores.StepStatusSuccess, ExitCode: 0, StartedAt: &started, FinishedAt: &finished},
			{Name: "bad", Status: stores.StepStatusFailed, ExitCode: 2, Error: "exit code 2", StartedAt: &started, FinishedAt: &finished},
		},
	}
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "execution_show", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "exit 2") {
		t.Errorf("expected the failing step exit code, got:\n%s", out)
	}
	if strings.Contains(out, "exit 0") {
		t.Errorf("did not expect a zero exit code on the passing step")
	}
}

func TestExecutionTemplateShowsSetupDuration(t *testing.T) {
	started := time.Now()
	setupFinished := started.Add(10 * time.Second)
	finished := started.Add(30 * time.Second)
	data := executionPage{
		Title: "Execution", Active: "projects", Version: "dev",
		Execution: stores.Execution{
			ExecutionID: 1, Attempt: 1, AttemptsCount: 1,
			Workflow: "build", Ref: "main", Status: stores.StatusSuccess,
			CreatedAt: started, AttemptCreatedAt: started, StartedAt: &started, SetupFinishedAt: &setupFinished, FinishedAt: &finished,
		},
		SetupLog: "cloning...",
	}
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "execution_show", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Setup") || !strings.Contains(out, "10s") {
		t.Fatalf("expected setup duration in page, got:\n%s", out)
	}
}

func TestExecutionTemplateRunningStepTimer(t *testing.T) {
	started := time.Now().Add(-30 * time.Second)
	data := executionPage{
		Title: "Execution", Active: "projects", Version: "dev",
		Execution: stores.Execution{
			ID: 42, ExecutionID: 42, Attempt: 1, AttemptsCount: 1, ProjectID: uuid.MustParse("33333333-3333-3333-3333-333333333333"),
			Workflow: "build", Ref: "main", Status: stores.StatusRunning,
			CreatedAt: started, AttemptCreatedAt: started, StartedAt: &started,
		},
		Steps: []stepView{
			{Name: "compile", Status: stores.StepStatusRunning, StartedAt: &started, Timeout: 5 * time.Minute},
		},
		Running: true,
	}
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "execution_show", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `data-step-time`) || !strings.Contains(out, `data-timeout="300000"`) {
		t.Fatalf("expected the running step timer and timeout in the header, got:\n%s", out)
	}
	if strings.Contains(out, " open>") {
		t.Fatalf("expected step blocks to be collapsed by default, got:\n%s", out)
	}
}

func TestExecutionTemplateFailedStepShowsExceededTimeout(t *testing.T) {
	started := time.Now().Add(-6 * time.Minute)
	finished := time.Now()
	data := executionPage{
		Title: "Execution", Active: "projects", Version: "dev",
		Execution: stores.Execution{
			ExecutionID: 1, Attempt: 1, AttemptsCount: 1,
			Workflow: "build", Ref: "main", Status: stores.StatusFailed,
			CreatedAt: started, AttemptCreatedAt: started, StartedAt: &started, FinishedAt: &finished,
		},
		Steps: []stepView{
			{Name: "compile", Status: stores.StepStatusFailed, StartedAt: &started, FinishedAt: &finished, Timeout: 5 * time.Minute},
			{Name: "test", Status: stores.StepStatusSuccess, StartedAt: &started, FinishedAt: &finished, Timeout: 5 * time.Minute},
		},
	}
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "execution_show", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "6m / 5m") {
		t.Fatalf("expected the failed step to show its timeout, got:\n%s", out)
	}
	if n := strings.Count(out, "/ 5m"); n != 1 {
		t.Fatalf("expected the timeout on the failed step only, got %d occurrences:\n%s", n, out)
	}
}

func TestExecutionTemplateStreamsWhenRunning(t *testing.T) {
	started := time.Now()
	data := executionPage{
		Title: "Execution", Active: "projects", Version: "dev",
		Execution: stores.Execution{
			ID: 42, ExecutionID: 42, Attempt: 1, AttemptsCount: 1, ProjectID: uuid.MustParse("33333333-3333-3333-3333-333333333333"),
			Workflow: "build", Ref: "main", Status: stores.StatusRunning,
			CreatedAt: started, AttemptCreatedAt: started, StartedAt: &started,
		},
		Steps: []stepView{
			{LogSource: "compile", Name: "compile", Status: stores.StepStatusRunning, StartedAt: &started},
		},
		Running: true,
	}
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "execution_show", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`data-source="setup"`,
		`data-source="compile"`,
		`new EventSource(`,
		"/stream?attempt=1",
		// The status vocabulary is rendered once by Go and read back by the
		// live script, which keeps no copy of the class table.
		`id="status-presentation"`,
		`data-status="success"`,
		`data-class="badge badge-soft badge-success"`,
		`data-label="Success"`,
		`labelFor(state.status)`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in running execution page, got:\n%s", want, out)
		}
	}
}

func TestExecutionTemplateShowsArtifactHashes(t *testing.T) {
	started := time.Now()
	projectID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	full := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	data := executionPage{
		Title: "Execution", Active: "projects", Version: "dev",
		Project: stores.Project{ID: projectID, Name: "demo"},
		Execution: stores.Execution{
			ID: 12, ExecutionID: 12, Attempt: 1, AttemptsCount: 1, ProjectID: projectID,
			Workflow: "build", Ref: "main", Status: stores.StatusSuccess,
			CreatedAt: started, AttemptCreatedAt: started, StartedAt: &started, FinishedAt: &started,
		},
		Artifacts: []artifacts.Artifact{
			{Step: "build", Path: "dist/app", Size: 5, Hash: &artifacts.Hash{Algorithm: "sha256", Value: full}},
			{Step: "build", Path: "dist/old", Size: 3},
		},
	}
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "execution_show", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{">Hash<", "2cf24dba5fb0", `title="` + full + `"`, `data-copy="` + full + `"`, ">Copy<", ">—<"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in execution page, got:\n%s", want, out)
		}
	}
}

func TestExecutionTemplateShowsAttemptHistory(t *testing.T) {
	started := time.Now()
	finished := started.Add(30 * time.Second)
	projectID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	data := executionPage{
		Title: "Execution", Active: "projects", Version: "dev",
		Project: stores.Project{ID: projectID, Name: "demo"},
		Execution: stores.Execution{
			ID: 12, ExecutionID: 7, Attempt: 2, AttemptsCount: 2, ProjectID: projectID,
			Workflow: "build", Ref: "main", Status: stores.StatusSuccess,
			CreatedAt: started.Add(-time.Hour), AttemptCreatedAt: started,
			StartedAt: &started, FinishedAt: &finished,
		},
		Attempts: []stores.Execution{
			{Attempt: 1, Status: stores.StatusFailed, Trigger: stores.TriggerManual, CreatedAt: started.Add(-time.Hour), Error: "step test failed"},
			{Attempt: 2, Status: stores.StatusSuccess, Trigger: stores.TriggerRetry, CreatedAt: started},
		},
		IsLatest: true,
	}
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "execution_show", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"attempt 2 of 2", "Attempt history", "?attempt=1", "?attempt=2", "step test failed", "retry"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in execution page, got:\n%s", want, out)
		}
	}
}

func TestExecutionTemplateOlderAttemptIsReadOnly(t *testing.T) {
	started := time.Now()
	projectID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	data := executionPage{
		Title: "Execution", Active: "projects", Version: "dev",
		Project: stores.Project{ID: projectID, Name: "demo"},
		Execution: stores.Execution{
			ID: 7, ExecutionID: 7, Attempt: 1, AttemptsCount: 2, ProjectID: projectID,
			Workflow: "build", Ref: "main", Status: stores.StatusFailed,
			CreatedAt: started, AttemptCreatedAt: started, StartedAt: &started,
		},
		Attempts: []stores.Execution{
			{Attempt: 1, Status: stores.StatusFailed, Trigger: stores.TriggerManual, CreatedAt: started},
			{Attempt: 2, Status: stores.StatusSuccess, Trigger: stores.TriggerRetry, CreatedAt: started},
		},
		IsLatest: false,
	}
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "execution_show", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Attempt 1 is read-only") {
		t.Fatalf("expected the older attempt to be read-only, got:\n%s", out)
	}
	for _, unwanted := range []string{"Retry failed steps", ">Rebuild<"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("did not expect %q on an older attempt", unwanted)
		}
	}
}

func TestSafeNext(t *testing.T) {
	cases := map[string]string{
		"":                    "",
		"/":                   "/",
		"/projects/demo":      "/projects/demo",
		"/projects?status=ok": "/projects?status=ok",
		"//evil.com":          "",
		"/\\evil.com":         "",
		"https://evil.com":    "",
		"javascript:alert(1)": "",
		"projects":            "",
	}
	for input, want := range cases {
		if got := safeNext(input); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLoginRequired(t *testing.T) {
	env := newTestEnv(t)
	client := env.anonymousClient(t)
	resp, err := client.Get(env.srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect to login, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); !strings.HasPrefix(got, "/login?next=") {
		t.Fatalf("expected /login?next=..., got %q", got)
	}
}

func TestLoginPageShowsGitHub(t *testing.T) {
	env := newTestEnv(t)
	client := env.anonymousClient(t)
	resp, err := client.Get(env.srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	out := body(t, resp)
	if !strings.Contains(out, "Sign in with GitHub") {
		t.Errorf("expected the GitHub sign-in button, got:\n%s", out)
	}
	if !strings.Contains(out, "Access is restricted") {
		t.Errorf("expected the access restriction notice, got:\n%s", out)
	}
	// The login page is public: it must not disclose the configured teams or
	// logins.
	for _, unwanted := range []string{"acme/pici", "alice"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("login page leaks %q:\n%s", unwanted, out)
		}
	}
}

func TestUsersOnlyLogin(t *testing.T) {
	env := newTestEnvWithAccess(t, nil, []string{"alice"}, func(p *fakeProvider) {
		p.identity.Scopes = nil
	})
	client := env.anonymousClient(t)

	page, err := client.Get(env.srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	out := body(t, page)
	if !strings.Contains(out, "Sign in with GitHub") || strings.Contains(out, "alice") {
		t.Fatalf("expected the users-only login page, got:\n%s", out)
	}

	start, err := client.Get(env.srv.URL + "/auth/github")
	if err != nil {
		t.Fatal(err)
	}
	defer start.Body.Close()
	location, _ := url.Parse(start.Header.Get("Location"))
	state := location.Query().Get("state")

	callback, err := client.Get(env.srv.URL + "/auth/github/callback?state=" + url.QueryEscape(state) + "&code=good")
	if err != nil {
		t.Fatal(err)
	}
	defer callback.Body.Close()
	if callback.StatusCode != http.StatusSeeOther || callback.Header.Get("Location") != "/" {
		t.Fatalf("expected redirect to /, got %d %q", callback.StatusCode, callback.Header.Get("Location"))
	}
	if env.provider.lastVerifier == "" {
		t.Fatal("expected the PKCE verifier to be sent back")
	}
	if cookieValue(client, env.srv.URL, auth.SessionCookie) == "" {
		t.Fatal("expected session cookie to be set")
	}
}

func TestOAuthLoginCreatesSession(t *testing.T) {
	env := newTestEnv(t)
	client := env.anonymousClient(t)

	start, err := client.Get(env.srv.URL + "/auth/github?next=/")
	if err != nil {
		t.Fatal(err)
	}
	defer start.Body.Close()
	if start.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect to GitHub, got %d", start.StatusCode)
	}
	location, err := url.Parse(start.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := location.Query().Get("state")
	if state == "" {
		t.Fatal("expected OAuth state")
	}

	callback, err := client.Get(env.srv.URL + "/auth/github/callback?state=" + url.QueryEscape(state) + "&code=good")
	if err != nil {
		t.Fatal(err)
	}
	defer callback.Body.Close()
	if callback.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect after callback, got %d: %s", callback.StatusCode, body(t, callback))
	}
	if got := callback.Header.Get("Location"); got != "/" {
		t.Fatalf("expected redirect to /, got %q", got)
	}
	if env.provider.lastVerifier == "" {
		t.Fatal("expected the PKCE verifier to be sent back")
	}
	if cookieValue(client, env.srv.URL, auth.SessionCookie) == "" {
		t.Fatal("expected session cookie to be set")
	}

	home, err := client.Get(env.srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if home.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with session cookie, got %d", home.StatusCode)
	}
}

func TestLoginRejectsNonMember(t *testing.T) {
	env := newTestEnv(t, func(p *fakeProvider) { p.teams = nil })
	client := env.anonymousClient(t)

	start, err := client.Get(env.srv.URL + "/auth/github")
	if err != nil {
		t.Fatal(err)
	}
	defer start.Body.Close()
	location, _ := url.Parse(start.Header.Get("Location"))
	state := location.Query().Get("state")

	callback, err := client.Get(env.srv.URL + "/auth/github/callback?state=" + url.QueryEscape(state) + "&code=good")
	if err != nil {
		t.Fatal(err)
	}
	defer callback.Body.Close()
	if callback.StatusCode != http.StatusSeeOther || callback.Header.Get("Location") != "/login?error=not_member" {
		t.Fatalf("expected not_member redirect, got %d %q", callback.StatusCode, callback.Header.Get("Location"))
	}
	if cookieValue(client, env.srv.URL, auth.SessionCookie) != "" {
		t.Fatal("session cookie must not be set for a non-member")
	}
}

func TestLoginRejectsMissingScope(t *testing.T) {
	env := newTestEnv(t, func(p *fakeProvider) { p.identity.Scopes = []string{"repo"} })
	client := env.anonymousClient(t)

	start, err := client.Get(env.srv.URL + "/auth/github")
	if err != nil {
		t.Fatal(err)
	}
	defer start.Body.Close()
	location, _ := url.Parse(start.Header.Get("Location"))
	state := location.Query().Get("state")

	callback, err := client.Get(env.srv.URL + "/auth/github/callback?state=" + url.QueryEscape(state) + "&code=good")
	if err != nil {
		t.Fatal(err)
	}
	defer callback.Body.Close()
	if callback.StatusCode != http.StatusSeeOther || callback.Header.Get("Location") != "/login?error=not_member" {
		t.Fatalf("expected not_member redirect, got %d %q", callback.StatusCode, callback.Header.Get("Location"))
	}
}

func TestLoginRejectsBadState(t *testing.T) {
	env := newTestEnv(t)
	client := env.anonymousClient(t)
	resp, err := client.Get(env.srv.URL + "/auth/github/callback?state=wrong&code=good")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login?error=state" {
		t.Fatalf("expected state error, got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestGitLabLoginFlow(t *testing.T) {
	env := newTestEnvWithAccess(t, []string{"acme/pici"}, nil, func(p *fakeProvider) {
		p.name = "gitlab"
	})
	client := env.anonymousClient(t)

	page, err := client.Get(env.srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	out := body(t, page)
	if !strings.Contains(out, "Sign in with GitLab") || !strings.Contains(out, "authorized group") {
		t.Fatalf("expected the GitLab login page, got:\n%s", out)
	}

	start, err := client.Get(env.srv.URL + "/auth/gitlab")
	if err != nil {
		t.Fatal(err)
	}
	defer start.Body.Close()
	if start.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect to GitLab, got %d", start.StatusCode)
	}
	location, err := url.Parse(start.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := location.Query().Get("state")
	if state == "" {
		t.Fatal("expected OAuth state")
	}

	callback, err := client.Get(env.srv.URL + "/auth/gitlab/callback?state=" + url.QueryEscape(state) + "&code=good")
	if err != nil {
		t.Fatal(err)
	}
	defer callback.Body.Close()
	if callback.StatusCode != http.StatusSeeOther || callback.Header.Get("Location") != "/" {
		t.Fatalf("expected redirect to /, got %d %q", callback.StatusCode, callback.Header.Get("Location"))
	}
	if cookieValue(client, env.srv.URL, auth.SessionCookie) == "" {
		t.Fatal("expected session cookie to be set")
	}
}

func TestOIDCLoginFlow(t *testing.T) {
	env := newTestEnvWithAccess(t, []string{"acme/pici"}, nil, func(p *fakeProvider) {
		p.name = "oidc"
	})
	client := env.anonymousClient(t)

	page, err := client.Get(env.srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	out := body(t, page)
	if !strings.Contains(out, "Sign in with SSO") || !strings.Contains(out, "authorized group") {
		t.Fatalf("expected the OIDC login page, got:\n%s", out)
	}

	start, err := client.Get(env.srv.URL + "/auth/oidc")
	if err != nil {
		t.Fatal(err)
	}
	defer start.Body.Close()
	if start.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect to the OIDC provider, got %d", start.StatusCode)
	}
	location, err := url.Parse(start.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := location.Query().Get("state")

	callback, err := client.Get(env.srv.URL + "/auth/oidc/callback?state=" + url.QueryEscape(state) + "&code=good")
	if err != nil {
		t.Fatal(err)
	}
	defer callback.Body.Close()
	if callback.StatusCode != http.StatusSeeOther || callback.Header.Get("Location") != "/" {
		t.Fatalf("expected redirect to /, got %d %q", callback.StatusCode, callback.Header.Get("Location"))
	}
	if cookieValue(client, env.srv.URL, auth.SessionCookie) == "" {
		t.Fatal("expected session cookie to be set")
	}
}

func TestAccountTokenLifecycle(t *testing.T) {
	env := newTestEnv(t)

	page, err := env.client.Get(env.srv.URL + "/account")
	if err != nil {
		t.Fatal(err)
	}
	if out := body(t, page); !strings.Contains(out, "Personal API tokens") {
		t.Fatalf("expected account page, got:\n%s", out)
	}

	created := postForm(t, env.client, env.srv.URL+"/account/tokens", url.Values{
		"name":    {"cli"},
		"expires": {"90d"},
	})
	createdBody := body(t, created)
	if created.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", created.StatusCode)
	}
	if !strings.Contains(createdBody, "pici_pat_") || !strings.Contains(createdBody, "Copy your new token now") {
		t.Fatalf("expected the new token to be shown once, got:\n%s", createdBody)
	}

	tokens, err := env.store.ListAPITokens(context.Background(), env.user.ID)
	if err != nil || len(tokens) != 1 || tokens[0].Name != "cli" {
		t.Fatalf("unexpected tokens: %+v (err=%v)", tokens, err)
	}

	renamed := postForm(t, env.client, env.srv.URL+"/account/tokens/"+tokens[0].ID.String()+"/rename", url.Values{"name": {"renamed"}})
	if renamed.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", renamed.StatusCode)
	}
	tokens, err = env.store.ListAPITokens(context.Background(), env.user.ID)
	if err != nil || len(tokens) != 1 || tokens[0].Name != "renamed" {
		t.Fatalf("token was not renamed: %+v (err=%v)", tokens, err)
	}

	revoked := postForm(t, env.client, env.srv.URL+"/account/tokens/"+tokens[0].ID.String()+"/delete", nil)
	if revoked.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", revoked.StatusCode)
	}
	tokens, err = env.store.ListAPITokens(context.Background(), env.user.ID)
	if err != nil || len(tokens) != 0 {
		t.Fatalf("expected token to be revoked, got %+v (err=%v)", tokens, err)
	}
}

func TestAccountSessionRevocation(t *testing.T) {
	env := newTestEnv(t)

	revoked := postForm(t, env.client, env.srv.URL+"/account/sessions/"+env.session.ID+"/delete", nil)
	if revoked.StatusCode != http.StatusSeeOther || revoked.Header.Get("Location") != "/login" {
		t.Fatalf("expected redirect to login, got %d %q", revoked.StatusCode, revoked.Header.Get("Location"))
	}

	home, err := env.client.Get(env.srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer home.Body.Close()
	if home.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected session to be revoked, got %d", home.StatusCode)
	}
}

func TestCSRFRequiredForMutations(t *testing.T) {
	env := newTestEnv(t)

	missing, err := env.client.PostForm(env.srv.URL+"/projects", url.Values{
		"name": {"demo"}, "repo_url": {"https://x/y.git"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without csrf, got %d", missing.StatusCode)
	}

	valid := postForm(t, env.client, env.srv.URL+"/projects", url.Values{
		"name": {"demo"}, "repo_url": {"https://x/y.git"}, "default_branch": {"main"},
	})
	defer valid.Body.Close()
	if valid.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 with csrf, got %d", valid.StatusCode)
	}
}

func TestExecutionPageShowsAttemptHistory(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	projectID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	if _, err := env.store.CreateProject(ctx, stores.Project{ID: projectID, Name: "demo", DefaultBranch: "main", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	started := time.Now().Add(-time.Hour)
	finished := started.Add(time.Minute)
	root := stores.Execution{
		ProjectID: projectID, Workflow: "build", Ref: "main", Status: stores.StatusFailed,
		CreatedAt: started, Steps: stores.Steps{{Name: "test", Status: stores.StepStatusFailed, Error: "boom"}},
	}
	if err := env.store.CreateExecution(ctx, &root); err != nil {
		t.Fatal(err)
	}
	root.StartedAt, root.FinishedAt, root.Error = &started, &finished, "step test failed"
	if err := env.store.UpdateExecution(ctx, root); err != nil {
		t.Fatal(err)
	}

	parentID, workspaceID := root.ID, root.ID
	retry := stores.Execution{
		ProjectID: projectID, ExecutionID: root.ID, Attempt: 2,
		Workflow: "build", Ref: "main", Status: stores.StatusSuccess, Trigger: stores.TriggerRetry,
		PreviousAttemptID: &parentID, WorkspaceID: &workspaceID, CreatedAt: time.Now(),
		Steps: stores.Steps{{Name: "test", Status: stores.StepStatusSuccess}},
	}
	if err := env.store.CreateExecution(ctx, &retry); err != nil {
		t.Fatal(err)
	}
	retry.StartedAt, retry.FinishedAt = &started, &finished
	if err := env.store.UpdateExecution(ctx, retry); err != nil {
		t.Fatal(err)
	}

	execURL := env.srv.URL + "/projects/" + projectID.String() + "/executions/" + strconv.FormatInt(root.ID, 10)
	resp, err := env.client.Get(execURL)
	if err != nil {
		t.Fatal(err)
	}
	out := body(t, resp)
	for _, want := range []string{"attempt 2 of 2", "Attempt history", "?attempt=1", "?attempt=2", "step test failed", "retry"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q on the latest attempt page, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Retry failed steps") {
		t.Errorf("did not expect a retry action on a successful attempt")
	}

	resp, err = env.client.Get(execURL + "?attempt=1")
	if err != nil {
		t.Fatal(err)
	}
	out = body(t, resp)
	if !strings.Contains(out, "Attempt 1 is read-only") {
		t.Errorf("expected the older attempt to be read-only, got:\n%s", out)
	}
	if strings.Contains(out, "Retry failed steps") || strings.Contains(out, ">Rebuild<") {
		t.Errorf("did not expect actions on an older attempt, got:\n%s", out)
	}
}

func TestExecutionNotFound(t *testing.T) {
	srv, client := newTestServer(t, "")
	resp, err := client.Get(srv.URL + "/projects/demo/executions/999")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestStaticAssetsServedWithoutAuth(t *testing.T) {
	srv, client := newTestServer(t, "secret")
	resp, err := client.Get(srv.URL + "/static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	_ = body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for static asset, got %d", resp.StatusCode)
	}
}

func TestWorkflowsTemplateRendersSelect(t *testing.T) {
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "workflows", struct {
		Workflows []string
		Error     string
	}{Workflows: []string{"build", "release"}}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `<select class="select w-full truncate" name="workflow"`) {
		t.Fatalf("expected a select, got:\n%s", out)
	}
	if !strings.Contains(out, `<option value="build">build</option>`) {
		t.Fatalf("expected build option, got:\n%s", out)
	}
}

func TestWorkflowsTemplateFallsBackToInput(t *testing.T) {
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "workflows", struct {
		Workflows []string
		Error     string
	}{Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `type="text" name="workflow"`) {
		t.Fatalf("expected a text input fallback, got:\n%s", out)
	}
	if !strings.Contains(out, "boom") {
		t.Fatalf("expected the error to be shown, got:\n%s", out)
	}
}

func TestFilterExecutions(t *testing.T) {
	execs := []stores.Execution{
		{Workflow: "build", Status: stores.StatusSuccess},
		{Workflow: "build", Status: stores.StatusFailed},
		{Workflow: "test", Status: stores.StatusSuccess},
	}
	if got := filterExecutions(execs, "", ""); len(got) != 3 {
		t.Fatalf("no filter: expected 3, got %d", len(got))
	}
	if got := filterExecutions(execs, "success", ""); len(got) != 2 {
		t.Fatalf("status filter: expected 2, got %d", len(got))
	}
	if got := filterExecutions(execs, "success", "build"); len(got) != 1 {
		t.Fatalf("status+workflow filter: expected 1, got %d", len(got))
	}
	if got := filterExecutions(execs, "running", ""); len(got) != 0 {
		t.Fatalf("no match: expected 0, got %d", len(got))
	}
	names := distinctWorkflows(execs)
	if len(names) != 2 || names[0] != "build" || names[1] != "test" {
		t.Fatalf("distinct workflows = %v", names)
	}
}

func TestRefsTemplateRendersSelect(t *testing.T) {
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "refs", struct {
		ProjectID string
		Branches  []string
		Tags      []string
		Default   string
		Error     string
	}{ProjectID: "abc", Branches: []string{"main"}, Tags: []string{"v1.0.0"}, Default: "main"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// djlint reflows the templates, so match attributes across line breaks.
	out = strings.Join(strings.Fields(out), " ")
	if !strings.Contains(out, `<optgroup label="Branches">`) || !strings.Contains(out, `<optgroup label="Tags">`) {
		t.Fatalf("expected branch and tag optgroups, got:\n%s", out)
	}
	if !strings.Contains(out, `<option value="main" selected>main</option>`) {
		t.Fatalf("expected default branch selected, got:\n%s", out)
	}
	if !strings.Contains(out, `hx-get="/projects/abc/configs"`) || !strings.Contains(out, `hx-target="#workflow-field"`) {
		t.Fatalf("expected the ref to refresh workflows, got:\n%s", out)
	}
}

func TestRefsTemplateFallsBackToInput(t *testing.T) {
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "refs", struct {
		ProjectID string
		Branches  []string
		Tags      []string
		Default   string
		Error     string
	}{ProjectID: "abc", Default: "main", Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// djlint reflows the templates, so match attributes across line breaks.
	out = strings.Join(strings.Fields(out), " ")
	if !strings.Contains(out, `type="text" name="ref"`) || !strings.Contains(out, "boom") {
		t.Fatalf("expected text input fallback with error, got:\n%s", out)
	}
	if !strings.Contains(out, `hx-get="/projects/abc/configs"`) {
		t.Fatalf("expected the ref input to refresh workflows, got:\n%s", out)
	}
}

func TestProjectDetect(t *testing.T) {
	repoDir := t.TempDir()
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("file.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("init", &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()}}); err != nil {
		t.Fatal(err)
	}

	srv, client := newTestServer(t, "")

	branchResp := postForm(t, client, srv.URL+"/projects/detect", url.Values{
		"repo_url":  {repoDir},
		"auth_type": {"none"},
	})
	if branchResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", branchResp.StatusCode)
	}
	if out := body(t, branchResp); !strings.Contains(out, `value="master"`) {
		t.Fatalf("expected detected default branch, got:\n%s", out)
	}

	userResp := postForm(t, client, srv.URL+"/projects/detect", url.Values{"auth_type": {"ssh"}})
	if out := body(t, userResp); !strings.Contains(out, `value="git"`) {
		t.Fatalf("expected ssh auth user default, got:\n%s", out)
	}
}

func TestWebhookSetupURL(t *testing.T) {
	cases := []struct {
		provider stores.Provider
		repoURL  string
		want     string
	}{
		{stores.ProviderGithub, "https://github.com/acme/demo.git", "https://github.com/acme/demo/settings/hooks/new"},
		{stores.ProviderGithub, "git@github.com:acme/demo.git", "https://github.com/acme/demo/settings/hooks/new"},
		{stores.ProviderGitlab, "https://gitlab.com/acme/demo.git", "https://gitlab.com/acme/demo/-/hooks"},
		{stores.ProviderGitlab, "git@gitlab.example.com:acme/demo.git", "https://gitlab.example.com/acme/demo/-/hooks"},
		{stores.ProviderGitlab, "http://gitlab.local:8080/acme/demo.git", "http://gitlab.local:8080/acme/demo/-/hooks"},
		{stores.ProviderGeneric, "https://github.com/acme/demo.git", ""},
		{stores.ProviderGithub, "", ""},
	}
	for _, tc := range cases {
		if got := webhookSetupURL(tc.provider, tc.repoURL); got != tc.want {
			t.Errorf("webhookSetupURL(%q, %q) = %q, want %q", tc.provider, tc.repoURL, got, tc.want)
		}
	}
}

func TestWebhookPayloadURL(t *testing.T) {
	h := &Handler{publicBaseURL: "https://pici.example.com"}
	cases := []struct {
		provider stores.Provider
		name     string
		want     string
	}{
		{stores.ProviderGithub, "demo", "https://pici.example.com/api/webhooks/github/demo"},
		{stores.ProviderGitlab, "", "https://pici.example.com/api/webhooks/gitlab/<project-name>"},
		{stores.ProviderGeneric, "demo", ""},
	}
	for _, tc := range cases {
		if got := h.webhookPayloadURL(tc.provider, tc.name); got != tc.want {
			t.Errorf("webhookPayloadURL(%q, %q) = %q, want %q", tc.provider, tc.name, got, tc.want)
		}
	}
}

func TestProjectFormWebhookHelp(t *testing.T) {
	env := newTestEnv(t)
	resp, err := env.client.Get(env.srv.URL + "/projects/new")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	out := body(t, resp)
	for _, want := range []string{
		`href="#webhook-help"`,
		`aria-label="Webhook setup instructions"`,
		`onclick="generateWebhookSecret(this)"`,
		"Webhooks are only supported for GitHub and GitLab projects.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the project form, got:\n%s", want, out)
		}
	}
}

func TestProjectDetectWebhookHelp(t *testing.T) {
	srv, client := newTestServer(t, "")
	resp := postForm(t, client, srv.URL+"/projects/detect", url.Values{
		"name":           {"demo"},
		"repo_url":       {"https://github.com/acme/demo.git"},
		"provider":       {"github"},
		"default_branch": {"main"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	out := body(t, resp)
	for _, want := range []string{
		`hx-swap-oob="innerHTML"`,
		"https://github.com/acme/demo/settings/hooks/new",
		"https://pici.test/api/webhooks/github/demo",
		"Let me select individual events",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the detect response, got:\n%s", want, out)
		}
	}
}

func TestProjectCreateWithKeyFile(t *testing.T) {
	env := newTestEnv(t)
	store := env.store

	key := "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n"
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range map[string]string{"_csrf": env.session.CSRFToken, "name": "demo", "repo_url": "https://example.com/x/y.git", "auth_type": "ssh", "default_branch": "main"} {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	fw, err := mw.CreateFormFile("auth_secret_file", "id_rsa")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(key)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodPost, env.srv.URL+"/projects", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = body(t, resp)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 after create, got %d", resp.StatusCode)
	}

	projects, err := store.ListProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].AuthSecret != key {
		t.Fatalf("auth secret from file not stored: %q", projects[0].AuthSecret)
	}
}

func TestSecretFieldsByAuthType(t *testing.T) {
	cases := []struct {
		authType string
		want     string
		notWant  string
	}{
		{"token", `type="password" name="auth_secret"`, `type="file"`},
		{"ssh", `type="file" name="auth_secret_file"`, `type="password"`},
		{"none", "", `name="auth_secret"`},
	}
	for _, tc := range cases {
		var buf strings.Builder
		if err := templates.ExecuteTemplate(&buf, "secret_fields_inner", projectFields{AuthType: stores.AuthType(tc.authType)}); err != nil {
			t.Fatal(err)
		}
		out := strings.Join(strings.Fields(buf.String()), " ")
		if tc.want != "" && !strings.Contains(out, tc.want) {
			t.Errorf("%s: expected %q, got %q", tc.authType, tc.want, out)
		}
		if strings.Contains(out, tc.notWant) {
			t.Errorf("%s: did not expect %q, got %q", tc.authType, tc.notWant, out)
		}
	}
}

func TestProjectShowExecutionShowsShortCommit(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	data := projectPage{
		Title: "demo", Active: "projects", Version: "dev",
		Project: stores.Project{ID: uuid.MustParse("33333333-3333-3333-3333-333333333333"), Name: "demo"},
		Executions: []stores.Execution{{
			ID: 7, ExecutionID: 7, Attempt: 1, AttemptsCount: 1, Workflow: "build", Ref: "main", Status: stores.StatusSuccess,
			CommitSHA: sha, CreatedAt: time.Now(),
		}},
	}
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "project_show", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `title="`+sha+`">0123456</span>`) {
		t.Fatalf("expected the short commit next to the ref, got:\n%s", out)
	}
}

func createProject(t *testing.T, client *http.Client, baseURL string) string {
	t.Helper()
	resp := postForm(t, client, baseURL+"/projects", url.Values{
		"name":           {"demo"},
		"repo_url":       {"https://github.com/acme/demo.git"},
		"provider":       {"github"},
		"auth_type":      {"none"},
		"default_branch": {"main"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create project: expected 303, got %d", resp.StatusCode)
	}
	return strings.TrimPrefix(resp.Header.Get("Location"), "/projects/")
}

func TestProjectCacheUnavailableWithoutDocker(t *testing.T) {
	srv, client := newTestServer(t, "")
	id := createProject(t, client, srv.URL)
	resp, err := client.Get(srv.URL + "/projects/" + id + "/cache")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(body(t, resp), "Docker is unavailable") {
		t.Fatal("expected a Docker unavailable notice")
	}
}

func TestProjectCacheTemplateRenders(t *testing.T) {
	data := projectCachePage{
		Title: "Cache", Active: "projects", Version: "dev",
		Project:     stores.Project{Name: "demo"},
		Images:      []cache.Image{{Reference: "pici/x-build", Size: 2048, Created: time.Now()}},
		Volumes:     []cache.Volume{{Cache: "node_modules", Name: "pici-cache-x-node_modules", Size: 4096, Created: time.Now()}},
		ImagesSize:  2048,
		VolumesSize: 4096,
		TotalSize:   6144,
	}
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, "project_cache", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"pici/x-build", "pici-cache-x-node_modules", "2.0 KB", "4.0 KB", "cache/images/delete", "cache/volumes/delete"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in page, got:\n%s", want, out)
		}
	}
}

func TestProjectCacheDeleteRejectsForeignResource(t *testing.T) {
	srv, client := newTestServer(t, "")
	id := createProject(t, client, srv.URL)

	img := postForm(t, client, srv.URL+"/projects/"+id+"/cache/images/delete", url.Values{"reference": {"pici/other-build"}})
	if img.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", img.StatusCode)
	}
	vol := postForm(t, client, srv.URL+"/projects/"+id+"/cache/volumes/delete", url.Values{"name": {"pici-cache-other-node_modules"}})
	if vol.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", vol.StatusCode)
	}

	shown, err := client.Get(srv.URL + "/projects/" + id + "/cache")
	if err != nil {
		t.Fatal(err)
	}
	_ = body(t, shown)
}

func TestProjectCacheDeleteRequiresCSRF(t *testing.T) {
	env := newTestEnv(t)
	created := postForm(t, env.client, env.srv.URL+"/projects", url.Values{
		"name": {"demo"}, "repo_url": {"https://github.com/acme/demo.git"}, "auth_type": {"none"}, "default_branch": {"main"},
	})
	id := strings.TrimPrefix(created.Header.Get("Location"), "/projects/")

	resp, err := env.client.PostForm(env.srv.URL+"/projects/"+id+"/cache/images/delete", url.Values{"reference": {"pici/x-build"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without csrf, got %d", resp.StatusCode)
	}
}

func TestArtifactDownloadSetsETag(t *testing.T) {
	const helloHash = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	store := newWebTestStore(t)
	ws := t.TempDir()
	h := newTestHandler(store, &ci.Runner{Store: store, Workspace: workspace.New(ws, filepath.Join(ws, "logs"))}, ws, nil, "", "dev")

	projectID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	if _, err := store.CreateProject(t.Context(), stores.Project{
		ID: projectID, Name: "demo", DefaultBranch: "main", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	execution := stores.Execution{
		ProjectID: projectID,
		Workflow:  "build",
		Ref:       "main",
		Steps:     stores.Steps{{Name: "build", Status: stores.StepStatusSuccess}},
	}
	if err := store.CreateExecution(t.Context(), &execution); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(ws, "artifacts", projectID.String(), strconv.FormatInt(execution.ID, 10))
	if err := os.MkdirAll(filepath.Join(root, "000", "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "000", "dist", "app"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"version":1,"artifacts":[{"path":"dist/app","hash":{"algorithm":"sha256","value":"` + helloHash + `"}}]}`
	if err := os.WriteFile(filepath.Join(root, "000.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	download := func(path string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetPathValue("id", projectID.String())
		req.SetPathValue("executionID", strconv.FormatInt(execution.ExecutionID, 10))
		req.SetPathValue("step", "build")
		req.SetPathValue("path", path)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rr := httptest.NewRecorder()
		h.ArtifactDownload(rr, req)
		return rr
	}

	rr := download("dist//app", nil)
	if rr.Code != http.StatusOK || rr.Body.String() != "hello" {
		t.Fatalf("expected 200 hello, got %d: %s", rr.Code, rr.Body.String())
	}
	if etag := rr.Header().Get("ETag"); etag != `"sha256-`+helloHash+`"` {
		t.Fatalf("unexpected ETag %q", etag)
	}

	rr = download("dist/app", map[string]string{"If-None-Match": `"sha256-` + helloHash + `"`})
	if rr.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d: %s", rr.Code, rr.Body.String())
	}
}

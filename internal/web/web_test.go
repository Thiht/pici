package web

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/handlers/middlewares"
	"github.com/Thiht/pici/internal/stores"
)

func newHandler(t *testing.T, token string) *Handler {
	t.Helper()
	store, err := stores.Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return New(store, &ci.Runner{Store: store}, t.TempDir(), token, "dev")
}

func newTestServer(t *testing.T, token string) (*httptest.Server, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(newHandler(t, token).Routes())
	t.Cleanup(srv.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return srv, client
}

func postForm(t *testing.T, client *http.Client, rawURL string, values url.Values) *http.Response {
	t.Helper()
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
	for _, name := range []string{"head", "nav", "foot", "flash", "login", "projects_list", "project_form", "project_fields", "secret_fields", "secret_fields_inner", "project_detect", "project_show", "project_variables", "project_cache", "workflows", "refs", "variables_list", "execution_show", "error"} {
		if templates.Lookup(name) == nil {
			t.Errorf("template %q not found", name)
		}
	}
}

func TestStatusClass(t *testing.T) {
	cases := map[string]string{
		"success":  "badge badge-soft badge-success",
		"failed":   "badge badge-soft badge-error",
		"running":  "badge badge-soft badge-info",
		"pending":  "badge badge-soft badge-warning",
		"canceled": "badge badge-soft badge-neutral",
		"skipped":  "badge badge-soft badge-neutral",
		"unknown":  "badge badge-soft badge-neutral",
	}
	for status, want := range cases {
		if got := statusClass(status); got != want {
			t.Errorf("statusClass(%q) = %q, want %q", status, got, want)
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
		"name":      {"demo"},
		"repo_url":  {"https://github.com/acme/demo.git"},
		"provider":  {"github"},
		"auth_type": {"none"},
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
		"name":      {"demo"},
		"repo_url":  {"https://github.com/acme/demo.git"},
		"provider":  {"github"},
		"auth_type": {"none"},
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
			Workflow: "build", Ref: "main", Status: stores.StatusFailed,
			CreatedAt: started, StartedAt: &started, FinishedAt: &finished,
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
			Workflow: "build", Ref: "main", Status: stores.StatusSuccess,
			CreatedAt: started, StartedAt: &started, SetupFinishedAt: &setupFinished, FinishedAt: &finished,
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

func TestExecutionTemplateStreamsWhenRunning(t *testing.T) {
	started := time.Now()
	data := executionPage{
		Title: "Execution", Active: "projects", Version: "dev",
		Execution: stores.Execution{
			ID: 42, ProjectID: uuid.MustParse("33333333-3333-3333-3333-333333333333"),
			Workflow: "build", Ref: "main", Status: stores.StatusRunning,
			CreatedAt: started, StartedAt: &started,
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
	for _, want := range []string{`data-source="setup"`, `data-source="compile"`, `new EventSource(`, "/stream"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in running execution page, got:\n%s", want, out)
		}
	}
}

func TestLoginRequired(t *testing.T) {
	srv, client := newTestServer(t, "secret")
	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect to login, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/login" {
		t.Fatalf("expected /login, got %q", got)
	}
}

func TestLoginSetsCookie(t *testing.T) {
	srv, client := newTestServer(t, "secret")

	page, err := client.Get(srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	if page.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 login page, got %d", page.StatusCode)
	}

	ok := postForm(t, client, srv.URL+"/login", url.Values{"token": {"secret"}})
	if ok.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 after login, got %d", ok.StatusCode)
	}
	if cookieValue(client, srv.URL, middlewares.SessionCookie) != "secret" {
		t.Fatal("expected session cookie to be set")
	}

	home, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if home.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with session cookie, got %d", home.StatusCode)
	}
}

func TestLoginWrongToken(t *testing.T) {
	srv, client := newTestServer(t, "secret")
	resp := postForm(t, client, srv.URL+"/login", url.Values{"token": {"nope"}})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestCSRFRequiredForMutations(t *testing.T) {
	srv, client := newTestServer(t, "secret")

	// Sign in, which also issues the CSRF cookie via the login page.
	if _, err := client.Get(srv.URL + "/login"); err != nil {
		t.Fatal(err)
	}
	if ok := postForm(t, client, srv.URL+"/login", url.Values{"token": {"secret"}}); ok.StatusCode != http.StatusSeeOther {
		t.Fatalf("login failed: %d", ok.StatusCode)
	}
	csrf := cookieValue(client, srv.URL, csrfCookie)
	if csrf == "" {
		t.Fatal("expected csrf cookie")
	}

	missing := postForm(t, client, srv.URL+"/projects", url.Values{"name": {"demo"}, "repo_url": {"https://x/y.git"}})
	if missing.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without csrf, got %d", missing.StatusCode)
	}

	valid := postForm(t, client, srv.URL+"/projects", url.Values{
		"_csrf": {csrf}, "name": {"demo"}, "repo_url": {"https://x/y.git"},
	})
	if valid.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 with csrf, got %d", valid.StatusCode)
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

func TestDefaultAuthUser(t *testing.T) {
	if got := defaultAuthUser(stores.AuthTypeToken); got != "oauth2" {
		t.Errorf("token auth user = %q", got)
	}
	if got := defaultAuthUser(stores.AuthTypeSsh); got != "git" {
		t.Errorf("ssh auth user = %q", got)
	}
	if got := defaultAuthUser(stores.AuthTypeNone); got != "" {
		t.Errorf("none auth user = %q", got)
	}
}

func TestProjectCreateWithKeyFile(t *testing.T) {
	store, err := stores.Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h := New(store, &ci.Runner{Store: store}, t.TempDir(), "", "dev")
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)

	key := "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n"
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range map[string]string{"name": "demo", "repo_url": "https://example.com/x/y.git", "auth_type": "ssh"} {
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

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/projects", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected final 200 after redirect, got %d", resp.StatusCode)
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

func TestCapitalize(t *testing.T) {
	cases := map[string]string{"success": "Success", "running": "Running", "": "", "x": "X"}
	for in, want := range cases {
		if got := capitalize(in); got != want {
			t.Errorf("capitalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func createProject(t *testing.T, client *http.Client, baseURL string) string {
	t.Helper()
	resp := postForm(t, client, baseURL+"/projects", url.Values{
		"name":      {"demo"},
		"repo_url":  {"https://github.com/acme/demo.git"},
		"provider":  {"github"},
		"auth_type": {"none"},
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
		Images:      []cacheImage{{Tag: "pici/x-build", Size: 2048, Created: time.Now()}},
		Volumes:     []cacheVolume{{Cache: "node_modules", Name: "pici-cache-x-node_modules", Size: 4096, Created: time.Now()}},
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
	srv, client := newTestServer(t, "secret")
	if _, err := client.Get(srv.URL + "/login"); err != nil {
		t.Fatal(err)
	}
	if ok := postForm(t, client, srv.URL+"/login", url.Values{"token": {"secret"}}); ok.StatusCode != http.StatusSeeOther {
		t.Fatalf("login failed: %d", ok.StatusCode)
	}
	csrf := cookieValue(client, srv.URL, csrfCookie)
	created := postForm(t, client, srv.URL+"/projects", url.Values{
		"_csrf": {csrf}, "name": {"demo"}, "repo_url": {"https://github.com/acme/demo.git"}, "auth_type": {"none"},
	})
	id := strings.TrimPrefix(created.Header.Get("Location"), "/projects/")

	resp := postForm(t, client, srv.URL+"/projects/"+id+"/cache/images/delete", url.Values{"reference": {"pici/x-build"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without csrf, got %d", resp.StatusCode)
	}
}

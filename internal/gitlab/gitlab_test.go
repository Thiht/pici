package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseRepo(t *testing.T) {
	cases := []struct {
		url  string
		base string
		path string
	}{
		{"https://gitlab.com/acme/demo.git", "https://gitlab.com", "acme/demo"},
		{"https://gitlab.example.com/group/sub/demo.git", "https://gitlab.example.com", "group/sub/demo"},
		{"git@gitlab.com:acme/demo.git", "https://gitlab.com", "acme/demo"},
		{"http://gitlab.local/acme/demo", "http://gitlab.local", "acme/demo"},
	}
	for _, tc := range cases {
		base, path, ok := ParseRepo(tc.url)
		if !ok || base != tc.base || path != tc.path {
			t.Errorf("ParseRepo(%q) = (%q, %q, %v), want (%q, %q, true)", tc.url, base, path, ok, tc.base, tc.path)
		}
	}
}

func TestSetCommitStatus(t *testing.T) {
	var gotPath, gotToken, gotState, gotName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotState = r.FormValue("state")
		gotName = r.FormValue("name")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	err := SetCommitStatus(context.Background(), srv.URL, "acme/demo", "tok", "abc123", "success", "pici/build", srv.URL+"/details", "")
	if err != nil {
		t.Fatalf("SetCommitStatus: %v", err)
	}
	if gotPath != "/api/v4/projects/acme%2Fdemo/statuses/abc123" {
		t.Errorf("unexpected path %q", gotPath)
	}
	if gotToken != "tok" || gotState != "success" || gotName != "pici/build" {
		t.Errorf("unexpected request: token=%q state=%q name=%q", gotToken, gotState, gotName)
	}
}

func TestSetCommitStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	err := SetCommitStatus(context.Background(), srv.URL, "acme/demo", "tok", "abc123", "failed", "pici/build", "", "")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected a 403 error, got %v", err)
	}
}

func TestMergeRequestDiffs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v4/projects/acme%2Fdemo/merge_requests/7/diffs" {
			t.Errorf("unexpected path %q", r.URL.EscapedPath())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"old_path":"a.go","new_path":"a.go"},{"old_path":"gone.go","new_path":""}]`))
	}))
	defer srv.Close()

	paths, err := MergeRequestDiffs(context.Background(), srv.URL, "acme/demo", "tok", 7)
	if err != nil {
		t.Fatalf("MergeRequestDiffs: %v", err)
	}
	if len(paths) != 2 || paths[0] != "a.go" || paths[1] != "gone.go" {
		t.Fatalf("unexpected paths %v", paths)
	}
}

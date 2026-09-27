// Package gitlab talks to the GitLab REST API (commit statuses, merge request
// diffs) using the project's stored token.
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Thiht/pici/internal/github"
)

const apiVersion = "api/v4"

// ParseRepo returns the scheme+host base URL and the "namespace/project" path
// for a GitLab remote, accepting any host so self-hosted instances work.
func ParseRepo(repoURL string) (base, path string, ok bool) {
	host, path, ok := github.ParseRemote(repoURL)
	if !ok || host == "" || path == "" {
		return "", "", false
	}
	scheme := "https"
	if i := strings.Index(repoURL, "://"); i >= 0 {
		scheme = repoURL[:i]
	}
	return scheme + "://" + host, path, true
}

// SetCommitStatus posts a commit status so it shows up on merge requests. The
// state is one of pending, running, success, failed, canceled.
func SetCommitStatus(ctx context.Context, base, path, token, sha, state, name, targetURL, description string) error {
	form := url.Values{"state": {state}, "name": {name}}
	if targetURL != "" {
		form.Set("target_url", targetURL)
	}
	if description != "" {
		form.Set("description", description)
	}
	endpoint := fmt.Sprintf("%s/%s/projects/%s/statuses/%s", strings.TrimRight(base, "/"), apiVersion, url.PathEscape(path), url.PathEscape(sha))
	_, err := post(ctx, endpoint, token, form)
	return err
}

// MergeRequestDiffs returns the paths changed by a merge request.
func MergeRequestDiffs(ctx context.Context, base, path, token string, iid int) ([]string, error) {
	endpoint := fmt.Sprintf("%s/%s/projects/%s/merge_requests/%d/diffs?per_page=100", strings.TrimRight(base, "/"), apiVersion, url.PathEscape(path), iid)
	body, err := get(ctx, endpoint, token)
	if err != nil {
		return nil, err
	}
	var diffs []struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
	if err := json.Unmarshal(body, &diffs); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(diffs))
	for _, d := range diffs {
		if d.NewPath != "" {
			paths = append(paths, d.NewPath)
		} else if d.OldPath != "" {
			paths = append(paths, d.OldPath)
		}
	}
	return paths, nil
}

func post(ctx context.Context, endpoint, token string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("PRIVATE-TOKEN", token)
	return do(req)
}

func get(ctx context.Context, endpoint, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	return do(req)
}

func do(req *http.Request) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gitlab %s %s: %d %s", req.Method, req.URL.Path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

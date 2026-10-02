// Package gitlab talks to the GitLab REST API (commit statuses, merge request
// diffs) using the project's stored token.
package gitlab

import (
	"context"
	"net/http"
	"strings"
	"time"

	gl "gitlab.com/gitlab-org/api/client-go"

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
	if before, _, ok := strings.Cut(repoURL, "://"); ok {
		scheme = before
	}
	return scheme + "://" + host, path, true
}

// SetCommitStatus posts a commit status so it shows up on merge requests. The
// state is one of pending, running, success, failed, canceled.
func SetCommitStatus(ctx context.Context, base, path, token, sha, state, name, targetURL, description string) error {
	client, err := newClient(base, token)
	if err != nil {
		return err
	}
	opt := &gl.SetCommitStatusOptions{State: gl.BuildStateValue(state), Name: &name}
	if targetURL != "" {
		opt.TargetURL = &targetURL
	}
	if description != "" {
		opt.Description = &description
	}
	_, _, err = client.Commits.SetCommitStatus(path, sha, opt, gl.WithContext(ctx))
	return err
}

// MergeRequestDiffs returns the paths changed by a merge request.
func MergeRequestDiffs(ctx context.Context, base, path, token string, iid int) ([]string, error) {
	client, err := newClient(base, token)
	if err != nil {
		return nil, err
	}
	diffs, _, err := client.MergeRequests.ListMergeRequestDiffs(path, int64(iid), &gl.ListMergeRequestDiffsOptions{
		PerPage: 100,
	}, gl.WithContext(ctx))
	if err != nil {
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

// newClient talks to the instance hosting the project. Requests are not
// retried: these calls report the state of an execution, where a delayed retry
// is worse than a failed report.
func newClient(base, token string) (*gl.Client, error) {
	return gl.NewClient(token,
		gl.WithBaseURL(strings.TrimRight(base, "/")+"/"+apiVersion),
		gl.WithCustomRetryMax(0),
		gl.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
	)
}

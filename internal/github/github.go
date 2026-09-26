package github

import (
	"net/url"
	"strings"
)

// parseRemote splits a git remote URL into its host and trimmed path,
// handling https, ssh and scp-like (git@host:owner/repo) syntaxes.
func parseRemote(repoURL string) (host, path string, ok bool) {
	s := strings.TrimSuffix(repoURL, ".git")
	if i := strings.Index(s, "://"); i >= 0 {
		u, err := url.Parse(s)
		if err != nil {
			return "", "", false
		}
		return u.Host, strings.Trim(u.Path, "/"), true
	}
	if at := strings.IndexByte(s, '@'); at >= 0 {
		if colon := strings.IndexByte(s[at:], ':'); colon >= 0 {
			return s[at+1 : at+colon], strings.Trim(s[at+colon+1:], "/"), true
		}
	}
	return "", strings.Trim(s, "/"), true
}

func ParseRepo(repoURL string) (owner, repo string, ok bool) {
	host, path, ok := parseRemote(repoURL)
	if !ok || (host != "github.com" && host != "www.github.com") {
		return "", "", false
	}
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// RepoSlug returns the "owner/repo" path of a git remote URL, for any host.
func RepoSlug(repoURL string) string {
	_, path, _ := parseRemote(repoURL)
	return path
}

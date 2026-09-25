package github

import (
	"net/url"
	"strings"
)

func ParseRepo(repoURL string) (owner, repo string, ok bool) {
	u, err := url.Parse(strings.TrimSuffix(repoURL, ".git"))
	if err != nil {
		return "", "", false
	}
	if u.Host != "github.com" && u.Host != "www.github.com" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

package github

import "testing"

func TestRepoSlug(t *testing.T) {
	tests := map[string]string{
		"https://github.com/Thiht/pici.git":     "Thiht/pici",
		"git@github.com:Thiht/pici.git":         "Thiht/pici",
		"ssh://git@github.com/Thiht/pici":       "Thiht/pici",
		"https://gitlab.com/group/sub/repo.git": "group/sub/repo",
		"git@gitlab.com:group/sub/repo.git":     "group/sub/repo",
		"https://git.example.com:8443/acme/ci":  "acme/ci",
	}
	for url, want := range tests {
		if got := RepoSlug(url); got != want {
			t.Errorf("RepoSlug(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestParseRepo(t *testing.T) {
	tests := map[string]string{
		"https://github.com/Thiht/pici.git": "Thiht/pici",
		"git@github.com:Thiht/pici.git":     "Thiht/pici",
	}
	for url, want := range tests {
		owner, repo, ok := ParseRepo(url)
		if !ok || owner+"/"+repo != want {
			t.Errorf("ParseRepo(%q) = %q/%q, %v; want %q", url, owner, repo, ok, want)
		}
	}
	if _, _, ok := ParseRepo("https://gitlab.com/group/repo.git"); ok {
		t.Error("ParseRepo should reject non-github URLs")
	}
}

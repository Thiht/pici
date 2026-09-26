package ci

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/Thiht/pici/internal/git"
	"github.com/Thiht/pici/internal/stores"
)

func projectCloneConfig(p stores.Project, dir, ref string) git.CloneConfig {
	return git.CloneConfig{
		URL:  p.RepoURL,
		Dir:  dir,
		Ref:  ref,
		Auth: git.Auth{Type: p.AuthType.String(), User: p.AuthUser, Secret: p.AuthSecret},
	}
}

func DiscoverProjectWorkflows(ctx context.Context, p stores.Project, dir, ref string) ([]string, error) {
	if err := git.Clone(ctx, projectCloneConfig(p, dir, ref)); err != nil {
		return nil, err
	}

	ciDir := filepath.Join(dir, ".ci")
	entries, err := os.ReadDir(ciDir)
	if err != nil {
		return nil, err
	}

	var workflows []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(ciDir, e.Name(), "ci.yml")); err == nil {
			workflows = append(workflows, e.Name())
		}
	}
	sort.Strings(workflows)
	return workflows, nil
}

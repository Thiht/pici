package ci

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/Thiht/pici/internal/git"
)

func DiscoverWorkflows(ctx context.Context, cloneCfg git.CloneConfig) ([]string, error) {
	if err := git.Clone(ctx, cloneCfg); err != nil {
		return nil, err
	}

	ciDir := filepath.Join(cloneCfg.Dir, ".ci")
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

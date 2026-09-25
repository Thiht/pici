package ci

import (
	"os"
	"path/filepath"
)

type cachePreset struct {
	markers []string
	env     map[string]string
	paths   []string
}

var cachePresets = []cachePreset{
	{
		markers: []string{"go.mod"},
		env:     map[string]string{"GOMODCACHE": ".cache/gomod", "GOCACHE": ".cache/gobuild"},
		paths:   []string{".cache/gomod", ".cache/gobuild"},
	},
	{
		markers: []string{"package.json"},
		env:     map[string]string{"npm_config_cache": ".npm"},
		paths:   []string{".npm"},
	},
	{
		markers: []string{"Cargo.toml"},
		env:     map[string]string{"CARGO_HOME": ".cargo"},
		paths:   []string{".cargo"},
	},
	{
		markers: []string{"requirements.txt", "pyproject.toml"},
		env:     map[string]string{"PIP_CACHE_DIR": ".cache/pip"},
		paths:   []string{".cache/pip"},
	},
}

func detectCaches(repoDir, mountPath string) (map[string]string, []string) {
	env := map[string]string{}
	var paths []string
	for _, preset := range cachePresets {
		found := false
		for _, marker := range preset.markers {
			if _, err := os.Stat(filepath.Join(repoDir, marker)); err == nil {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		for k, v := range preset.env {
			env[k] = filepath.Join(mountPath, v)
		}
		paths = append(paths, preset.paths...)
	}
	return env, paths
}

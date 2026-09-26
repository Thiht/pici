package ci

import (
	"os"
	"path/filepath"
)

// CacheMountPath is where auto-detected dependency caches are mounted inside the
// runner container. It sits outside the repo mount so caches never pollute the
// working tree, where any tool scanning the repo would otherwise walk them.
const CacheMountPath = "/pici/cache"

type cachePreset struct {
	markers []string
	// env maps an environment variable to its cache path, relative to the cache
	// root (see CacheMountPath).
	env map[string]string
	// paths are the cache paths to persist, relative to the cache root.
	paths []string
}

var cachePresets = []cachePreset{
	{
		markers: []string{"go.mod"},
		env:     map[string]string{"GOMODCACHE": "gomod", "GOCACHE": "gobuild"},
		paths:   []string{"gomod", "gobuild"},
	},
	{
		markers: []string{"package.json"},
		env:     map[string]string{"npm_config_cache": "npm"},
		paths:   []string{"npm"},
	},
	{
		markers: []string{"Cargo.toml"},
		env:     map[string]string{"CARGO_HOME": "cargo"},
		paths:   []string{"cargo"},
	},
	{
		markers: []string{"requirements.txt", "pyproject.toml"},
		env:     map[string]string{"PIP_CACHE_DIR": "pip"},
		paths:   []string{"pip"},
	},
}

// detectCaches returns the env vars to set and the cache paths to persist for a
// repo, both relative to cacheRoot.
func detectCaches(repoDir, cacheRoot string) (map[string]string, []string) {
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
		for k, rel := range preset.env {
			env[k] = filepath.Join(cacheRoot, rel)
		}
		paths = append(paths, preset.paths...)
	}
	return env, paths
}

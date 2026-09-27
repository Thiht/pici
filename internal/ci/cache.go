package ci

import (
	"path/filepath"
	"strings"
)

// CacheMountPath is where auto-detected dependency caches are mounted inside the
// runner container. It sits outside the repo mount so caches never pollute the
// working tree, where any tool scanning the repo would otherwise walk them.
const CacheMountPath = "/pici/cache"

// cacheToken is replaced by the absolute cache root in env values, so a preset
// can either point an env var at a directory or embed the path in a flag.
const cacheToken = "{cache}"

type cachePreset struct {
	// markers are glob patterns, relative to the repo root, whose presence
	// enables the preset.
	markers []string
	// env maps an environment variable to its value, with cacheToken standing
	// for the absolute cache root (see CacheMountPath).
	env map[string]string
	// paths are the cache paths to persist, relative to the cache root.
	paths []string
}

var cachePresets = []cachePreset{
	{
		markers: []string{"go.mod"},
		env:     map[string]string{"GOMODCACHE": cacheToken + "/gomod", "GOCACHE": cacheToken + "/gobuild"},
		paths:   []string{"gomod", "gobuild"},
	},
	{
		markers: []string{"package.json"},
		env:     map[string]string{"npm_config_cache": cacheToken + "/npm"},
		paths:   []string{"npm"},
	},
	{
		markers: []string{"yarn.lock"},
		env:     map[string]string{"YARN_CACHE_FOLDER": cacheToken + "/yarn"},
		paths:   []string{"yarn"},
	},
	{
		markers: []string{"pnpm-lock.yaml"},
		env:     map[string]string{"npm_config_store_dir": cacheToken + "/pnpm"},
		paths:   []string{"pnpm"},
	},
	{
		markers: []string{"bun.lock", "bun.lockb"},
		env:     map[string]string{"BUN_INSTALL": cacheToken + "/bun"},
		paths:   []string{"bun"},
	},
	{
		markers: []string{"deno.json", "deno.jsonc"},
		env:     map[string]string{"DENO_DIR": cacheToken + "/deno"},
		paths:   []string{"deno"},
	},
	{
		markers: []string{"Cargo.toml"},
		env:     map[string]string{"CARGO_HOME": cacheToken + "/cargo"},
		paths:   []string{"cargo"},
	},
	{
		markers: []string{"requirements.txt", "pyproject.toml"},
		env:     map[string]string{"PIP_CACHE_DIR": cacheToken + "/pip"},
		paths:   []string{"pip"},
	},
	{
		markers: []string{"Gemfile"},
		env:     map[string]string{"BUNDLE_PATH": cacheToken + "/bundle"},
		paths:   []string{"bundle"},
	},
	{
		markers: []string{"composer.json"},
		env:     map[string]string{"COMPOSER_CACHE_DIR": cacheToken + "/composer"},
		paths:   []string{"composer"},
	},
	{
		markers: []string{"pom.xml"},
		env:     map[string]string{"MAVEN_OPTS": "-Dmaven.repo.local=" + cacheToken + "/m2"},
		paths:   []string{"m2"},
	},
	{
		markers: []string{"build.gradle", "build.gradle.kts"},
		env:     map[string]string{"GRADLE_USER_HOME": cacheToken + "/gradle"},
		paths:   []string{"gradle"},
	},
	{
		markers: []string{"*.csproj", "*.sln"},
		env:     map[string]string{"NUGET_PACKAGES": cacheToken + "/nuget"},
		paths:   []string{"nuget"},
	},
	{
		markers: []string{"*.tf"},
		env:     map[string]string{"TF_PLUGIN_CACHE_DIR": cacheToken + "/terraform"},
		paths:   []string{"terraform"},
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
			if matches, _ := filepath.Glob(filepath.Join(repoDir, marker)); len(matches) > 0 {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		for k, v := range preset.env {
			env[k] = strings.ReplaceAll(v, cacheToken, cacheRoot)
		}
		paths = append(paths, preset.paths...)
	}
	return env, paths
}
